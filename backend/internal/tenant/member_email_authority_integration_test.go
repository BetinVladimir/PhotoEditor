package tenant

import (
	"context"
	"crypto/rand"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/domain"
	"testing"
	"time"
)

// Actual nonprivileged LOGIN attestation; never grant the API SELECT on users.
func memberProjectionRuntime(t *testing.T, admin *pgxpool.Pool) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	name := "wire_member_api_" + uuid.NewString()[:8]
	password := uuid.NewString()
	if _, err := admin.Exec(ctx, "CREATE ROLE "+pgx.Identifier{name}.Sanitize()+" LOGIN NOINHERIT NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOCREATEDB NOREPLICATION PASSWORD '"+password+"'"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), "DROP ROLE "+pgx.Identifier{name}.Sanitize()); err != nil {
			t.Error(err)
		}
	})
	if _, err := admin.Exec(ctx, "GRANT photo_api TO "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	cfg := admin.Config().Copy()
	cfg.ConnConfig.User = name
	cfg.ConnConfig.Password = password
	cfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	database.GuardApplicationRole(cfg)
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err = pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestIntegrationMemberProjectionStrictRuntimeListAndCAS(t *testing.T) {
	s, admin, who, tid, target := tenantFixture(t)
	s.DB = memberProjectionRuntime(t, admin)
	// Match the production requirement for a configured pagination signing key.
	// This ephemeral key belongs only to this isolated test service.
	s.Cursors.Key = make([]byte, 32)
	if _, err := rand.Read(s.Cursors.Key); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var broad bool
	if err := s.DB.QueryRow(ctx, "SELECT has_table_privilege(current_user,'photo.users','SELECT')").Scan(&broad); err != nil || broad {
		t.Fatal("API gained global user read", err)
	}
	page, err := s.ListMembers(ctx, who, tid, 1, "")
	if err != nil || len(page.Items) != 1 || page.NextCursor == nil {
		t.Fatal("first actual page", err)
	}
	second, err := s.ListMembers(ctx, who, tid, 1, *page.NextCursor)
	if err != nil || len(second.Items) != 1 || second.NextCursor != nil || second.Items[0].ID == page.Items[0].ID {
		t.Fatal("actual cursor page", err)
	}
	for _, item := range append(page.Items, second.Items...) {
		var email string
		if err = admin.QueryRow(ctx, "SELECT email_normalized FROM photo.users WHERE id=$1", item.UserID).Scan(&email); err != nil || item.Email != email {
			t.Fatal("member email identity mismatch", err)
		}
	}
	changed, err := s.UpdateMember(ctx, who, tid, target, 1, domain.Viewer, "suspended")
	if err != nil || changed.UserID != target || changed.Role != domain.Viewer || changed.Status != "suspended" || changed.Revision != "2" || changed.Email == "" {
		t.Fatal("real CAS receipt", err)
	}
	all, err := s.ListMembers(ctx, who, tid, 100, "")
	if err != nil || len(all.Items) != 2 {
		t.Fatal("suspended target remains visible", err)
	}
	if _, err = s.UpdateMember(ctx, who, tid, target, 1, domain.Editor, "active"); domain.AsError(err).Code != "REVISION_CONFLICT" {
		t.Fatal("stale CAS weakened", err)
	}
	if _, err = s.UpdateMember(ctx, who, tid, who.UserID, 1, domain.Viewer, "suspended"); domain.AsError(err).Code != "OWNER_PROTECTED" {
		t.Fatal("owner guard weakened", err)
	}
}

func TestIntegrationMemberEmailProjectionRefusalsNoWrite(t *testing.T) {
	_, admin, who, tid, target := tenantFixture(t)
	_, _, foreign, foreignTenant, foreignTarget := tenantFixture(t)
	runtime := memberProjectionRuntime(t, admin)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	snapshot := func() string {
		var value string
		err := admin.QueryRow(ctx, `SELECT coalesce(jsonb_agg(to_jsonb(m) ORDER BY m.id)::text,'[]') FROM photo.memberships m WHERE tenant_id=ANY($1::uuid[])`, []uuid.UUID{tid, foreignTenant}).Scan(&value)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	type vector struct {
		name                           string
		guc                            string
		actor, session, tenant, target *uuid.UUID
	}
	ptr := func(id uuid.UUID) *uuid.UUID { return &id }
	zero := uuid.Nil
	missing := uuid.New()
	vectors := []vector{
		{"missing-guc", "", ptr(who.UserID), ptr(who.SessionID), ptr(tid), ptr(target)},
		{"foreign-guc", foreignTenant.String(), ptr(who.UserID), ptr(who.SessionID), ptr(tid), ptr(target)},
		{"null-actor", tid.String(), nil, ptr(who.SessionID), ptr(tid), ptr(target)},
		{"zero-actor", tid.String(), ptr(zero), ptr(who.SessionID), ptr(tid), ptr(target)},
		{"wrong-session", tid.String(), ptr(who.UserID), ptr(foreign.SessionID), ptr(tid), ptr(target)},
		{"missing-session", tid.String(), ptr(who.UserID), ptr(missing), ptr(tid), ptr(target)},
		{"foreign-actor", tid.String(), ptr(foreign.UserID), ptr(foreign.SessionID), ptr(tid), ptr(target)},
		{"foreign-target", tid.String(), ptr(who.UserID), ptr(who.SessionID), ptr(tid), ptr(foreignTarget)},
		{"missing-target", tid.String(), ptr(who.UserID), ptr(who.SessionID), ptr(tid), ptr(missing)},
		{"wrong-tenant", foreignTenant.String(), ptr(who.UserID), ptr(who.SessionID), ptr(foreignTenant), ptr(target)},
	}
	refuse := func(t *testing.T, v vector) {
		t.Helper()
		before := snapshot()
		tx, err := runtime.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", v.guc); err != nil {
			t.Fatal(err)
		}
		var email string
		err = tx.QueryRow(ctx, "SELECT photo.tenant_member_email($1,$2,$3,$4)", v.actor, v.session, v.tenant, v.target).Scan(&email)
		var pgerr *pgconn.PgError
		if !errors.As(err, &pgerr) || pgerr.Code != "42501" || pgerr.Message != "FORBIDDEN" {
			t.Fatal("exact private projection refusal required", err)
		}
		if err = tx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		if snapshot() != before {
			t.Fatal("projection mutated membership state")
		}
	}
	for _, v := range vectors {
		t.Run(v.name, func(t *testing.T) { refuse(t, v) })
	}
	if _, err := admin.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	t.Run("revoked-session", func(t *testing.T) {
		refuse(t, vector{"revoked", tid.String(), ptr(who.UserID), ptr(who.SessionID), ptr(tid), ptr(target)})
	})
	// Existing service entry/final authority still rejects the revoked actor.
	s := Service{DB: runtime}
	if _, err := s.ListMembers(ctx, auth.Identity{UserID: who.UserID, SessionID: who.SessionID}, tid, 100, ""); err == nil {
		t.Fatal("revoked service read escaped")
	}
}

// An accepted self-change intentionally need not retain administrative authority afterward.
func TestIntegrationMemberProjectionAdminSelfChanges(t *testing.T) {
	for _, status := range []string{"active", "suspended"} {
		t.Run(status, func(t *testing.T) {
			s, admin, owner, tid, target := tenantFixture(t)
			s.DB = memberProjectionRuntime(t, admin)
			ctx := context.Background()
			promoted, err := s.UpdateMember(ctx, owner, tid, target, 1, domain.Admin, "active")
			if err != nil || promoted.Revision != "2" {
				t.Fatal("promotion", err)
			}
			sid := uuid.New()
			_, err = admin.Exec(ctx, "INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',$5)", sid, target, auth.HashSecret(uuid.NewString()), uuid.New(), time.Now().Add(time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			actor := auth.Identity{UserID: target, SessionID: sid}
			changed, err := s.UpdateMember(ctx, actor, tid, target, 2, domain.Viewer, status)
			if err != nil || changed.UserID != target || changed.Role != domain.Viewer || changed.Status != status || changed.Revision != "3" || changed.Email != promoted.Email {
				t.Fatal("self-change receipt", changed, err)
			}
			var role, actualStatus string
			var revision int64
			if err = admin.QueryRow(ctx, "SELECT role,status,revision FROM photo.memberships WHERE tenant_id=$1 AND user_id=$2", tid, target).Scan(&role, &actualStatus, &revision); err != nil || role != "viewer" || actualStatus != status || revision != 3 {
				t.Fatal("self-change not committed", err)
			}
			if _, err = s.UpdateMember(ctx, actor, tid, target, 3, domain.Editor, "active"); err == nil {
				t.Fatal("lost admin authority accepted")
			}
			if err = admin.QueryRow(ctx, "SELECT revision FROM photo.memberships WHERE tenant_id=$1 AND user_id=$2", tid, target).Scan(&revision); err != nil || revision != 3 {
				t.Fatal("refusal wrote membership", err)
			}
		})
	}
}
func TestIntegrationMemberProjectionForeignActorUpdateRefused(t *testing.T) {
	s, admin, _, tid, target := tenantFixture(t)
	_, _, foreign, _, _ := tenantFixture(t)
	s.DB = memberProjectionRuntime(t, admin)
	ctx := context.Background()
	var before, after string
	query := "SELECT to_jsonb(m)::text FROM photo.memberships m WHERE tenant_id=$1 AND user_id=$2"
	if err := admin.QueryRow(ctx, query, tid, target).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateMember(ctx, foreign, tid, target, 1, domain.Viewer, "suspended"); err == nil {
		t.Fatal("foreign actor accepted")
	}
	if err := admin.QueryRow(ctx, query, tid, target).Scan(&after); err != nil || after != before {
		t.Fatal("foreign refusal mutated target", err)
	}
}
