package tenant

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"testing"
	"time"
)

// The invitation and encrypted mail are produced by actual Service.Invite, not seeded accepted receipts.
func acceptedInviteFixture(t *testing.T) (*Service, *pgxpool.Pool, auth.Identity, uuid.UUID, uuid.UUID, string) {
	t.Helper()
	s, admin, owner, tid, target := tenantFixture(t)
	ctx := context.Background()
	s.MailKeys = auth.KeyRing{Active: "test", Keys: map[string][]byte{"test": make([]byte, 32)}}
	var email string
	if err := admin.QueryRow(ctx, "SELECT email_normalized FROM photo.users WHERE id=$1", target).Scan(&email); err != nil {
		t.Fatal(err)
	}
	invite, err := s.Invite(ctx, owner, tid, uuid.NewString(), InviteRequest{Email: email, Role: domain.Editor, Locale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	var id uuid.UUID
	var cipher []byte
	var kid string
	if err = admin.QueryRow(ctx, "SELECT id,payload_cipher,key_id FROM photo.mail_payloads WHERE invitation_id=$1", invite.ID).Scan(&id, &cipher, &kid); err != nil {
		t.Fatal(err)
	}
	raw, err := s.MailKeys.Open(kid, cipher, []byte(id.String()+":payload"))
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Token string `json:"token"`
	}
	if err = json.Unmarshal(raw, &payload); err != nil || payload.Token == "" {
		t.Fatal("mail token unavailable")
	}
	actor := auth.Identity{UserID: target, SessionID: uuid.New()}
	if _, err = admin.Exec(ctx, "INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',$5)", actor.SessionID, target, auth.HashSecret(uuid.NewString()), uuid.New(), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	return s, admin, actor, tid, invite.ID, payload.Token
}
func acceptState(t *testing.T, db *pgxpool.Pool, tid uuid.UUID) string {
	t.Helper()
	var value string
	err := db.QueryRow(context.Background(), `SELECT jsonb_build_object('tenant',(SELECT to_jsonb(t) FROM photo.tenants t WHERE id=$1),'members',(SELECT coalesce(jsonb_agg(to_jsonb(m) ORDER BY id),'[]') FROM photo.memberships m WHERE tenant_id=$1),'invitations',(SELECT coalesce(jsonb_agg(to_jsonb(i) - 'token_hash' ORDER BY id),'[]') FROM photo.invitations i WHERE tenant_id=$1),'audit',(SELECT count(*) FROM photo.audit_log WHERE tenant_id=$1 AND action='invitation.accept'))::text`, tid).Scan(&value)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func TestIntegrationAcceptInvitationPositiveAndReplay(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "existing"}[existing], func(t *testing.T) {
			s, admin, actor, tid, _, token := acceptedInviteFixture(t)
			ctx := context.Background()
			if !existing {
				if _, err := admin.Exec(ctx, "DELETE FROM photo.memberships WHERE tenant_id=$1 AND user_id=$2", tid, actor.UserID); err != nil {
					t.Fatal(err)
				}
			}
			first, err := s.AcceptInvite(ctx, actor, InviteAccept{Token: token})
			if err != nil || first.ID == uuid.Nil || first.TenantID != tid || first.UserID != actor.UserID || first.Role != domain.Editor || first.Status != "active" {
				t.Fatal("accept receipt invalid", err)
			}
			before := acceptState(t, admin, tid)
			second, err := s.AcceptInvite(ctx, actor, InviteAccept{Token: token})
			if err != nil || second != first || acceptState(t, admin, tid) != before {
				t.Fatal("replay changed receipt/state", err)
			}
			var count int
			if err = admin.QueryRow(ctx, "SELECT count(*) FROM photo.audit_log WHERE tenant_id=$1 AND action='invitation.accept'", tid).Scan(&count); err != nil || count != 1 {
				t.Fatal("duplicate accept event", err)
			}
		})
	}
}
func TestIntegrationAcceptInvitationInvalidIdentityAndExpiry(t *testing.T) {
	for _, change := range []string{"foreign_session", "foreign_user", "expired_invitation", "revoked_session", "expired_session"} {
		t.Run(change, func(t *testing.T) {
			s, admin, actor, tid, id, token := acceptedInviteFixture(t)
			_, _, foreign, _, _ := tenantFixture(t)
			ctx := context.Background()
			switch change {
			case "foreign_session":
				actor.SessionID = foreign.SessionID
			case "foreign_user":
				actor = foreign
			case "expired_invitation":
				if _, err := admin.Exec(ctx, "UPDATE photo.invitations SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", id); err != nil {
					t.Fatal(err)
				}
			case "revoked_session":
				if _, err := admin.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", actor.SessionID); err != nil {
					t.Fatal(err)
				}
			case "expired_session":
				if _, err := admin.Exec(ctx, "UPDATE photo.sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", actor.SessionID); err != nil {
					t.Fatal(err)
				}
			}
			before := acceptState(t, admin, tid)
			if _, err := s.AcceptInvite(ctx, actor, InviteAccept{Token: token}); domain.AsError(err).Code != "INVITATION_INVALID" {
				t.Fatal("wrong refusal", err)
			}
			if acceptState(t, admin, tid) != before {
				t.Fatal("invalid identity changed state")
			}
		})
	}
}
func TestIntegrationAcceptInvitationWaitRechecksAuthority(t *testing.T) {
	for _, replay := range []bool{false, true} {
		for _, lock := range []string{"invitation", "tenant"} {
			for _, change := range []string{"revoke", "expire", "disabled", "email"} {
				t.Run(map[bool]string{false: "fresh", true: "replay"}[replay]+"/"+lock+"/"+change, func(t *testing.T) {
					s, admin, actor, tid, id, token := acceptedInviteFixture(t)
					ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
					defer cancel()
					if replay {
						if _, err := s.AcceptInvite(ctx, actor, InviteAccept{Token: token}); err != nil {
							t.Fatal(err)
						}
					}
					holder, err := admin.Begin(ctx)
					if err != nil {
						t.Fatal(err)
					}
					defer holder.Rollback(context.Background())
					var pid int
					if err = holder.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
						t.Fatal(err)
					}
					query := "SELECT id FROM photo.invitations WHERE id=$1 FOR UPDATE"
					key := id
					if lock == "tenant" {
						query = "SELECT id FROM photo.tenants WHERE id=$1 FOR UPDATE"
						key = tid
					}
					var got uuid.UUID
					if err = holder.QueryRow(ctx, query, key).Scan(&got); err != nil {
						t.Fatal(err)
					}
					done := make(chan error, 1)
					go func() { _, e := s.AcceptInvite(ctx, actor, InviteAccept{Token: token}); done <- e }()
					awaitAdminLock(t, ctx, admin, pid, done)
					switch change {
					case "revoke":
						_, err = admin.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", actor.SessionID)
					case "expire":
						_, err = admin.Exec(ctx, "UPDATE photo.sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", actor.SessionID)
					case "disabled":
						_, err = admin.Exec(ctx, "UPDATE photo.users SET status='disabled' WHERE id=$1", actor.UserID)
					case "email":
						_, err = admin.Exec(ctx, "UPDATE photo.users SET email_normalized=$2 WHERE id=$1", actor.UserID, uuid.NewString()+"@changed.test")
					}
					if err != nil {
						t.Fatal(err)
					}
					before := acceptState(t, admin, tid)
					if err = holder.Commit(ctx); err != nil {
						t.Fatal(err)
					}
					if err = <-done; domain.AsError(err).Code != "INVITATION_INVALID" {
						t.Fatal("waited authority accepted", err)
					}
					if acceptState(t, admin, tid) != before {
						t.Fatal("wait refusal changed state")
					}
				})
			}
		}
	}
}
