package run

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/style"
	"photoeditor/backend/internal/tenant"
)

// Uses real OTP delivery encryption, session authentication, invitation acceptance
// and domain services. A Style shell is not an accepted StyleV2 version.
func TestIntegrationProvisionGenuineEditorPrincipals(t *testing.T) {
	dsn, output := os.Getenv("PHOTO_TEST_DATABASE_URL"), os.Getenv("PHOTO_EDITOR_PRINCIPALS_OUTPUT")
	if dsn == "" || output == "" {
		t.Skip("requires owned isolated database and private output")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	authDB, apiDB := genuineDedicatedPool(t, ctx, "auth"), genuineDedicatedPool(t, ctx, "api")
	pub, priv, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	key := make([]byte, 32)
	if _, e = rand.Read(key); e != nil {
		t.Fatal(e)
	}
	ring := auth.KeyRing{Active: "owned-fixture", Keys: map[string][]byte{"owned-fixture": key}}
	a := &auth.Service{DB: authDB, Pepper: key, MailKeys: ring, Signer: auth.TokenSigner{Active: "owned-fixture", Private: priv, Public: map[string]ed25519.PublicKey{"owned-fixture": pub}, Issuer: "owned-fixture", Audience: "api"}, Registration: true}
	mail := func(query string, arg any) map[string]string {
		var id uuid.UUID
		var kid string
		var cipher []byte
		if e := authDB.QueryRow(ctx, query, arg).Scan(&id, &kid, &cipher); e != nil {
			t.Fatal(e)
		}
		plain, e := ring.Open(kid, cipher, []byte(id.String()+":payload"))
		if e != nil {
			t.Fatal(e)
		}
		var v map[string]string
		if e = json.Unmarshal(plain, &v); e != nil {
			t.Fatal(e)
		}
		return v
	}
	issue := func(email string) auth.Identity {
		c, e := a.RequestOTP(ctx, email, "en", uuid.NewString())
		if e != nil {
			t.Fatal(e)
		}
		m := mail(`SELECT id,key_id,payload_cipher FROM photo.mail_payloads WHERE challenge_id=$1`, c.ID)
		token, e := a.VerifyOTP(ctx, c.ID, m["code"], "native")
		if e != nil {
			t.Fatal(e)
		}
		who, e := a.AuthenticateAccess(ctx, token.Access)
		if e != nil {
			t.Fatal(e)
		}
		return who
	}
	actor := issue(uuid.NewString() + "@example.test")
	foreignEmail := uuid.NewString() + "@example.test"
	foreign := issue(foreignEmail)
	tenants := &tenant.Service{DB: apiDB, MailKeys: ring}
	ten, e := tenants.Create(ctx, actor, uuid.NewString(), tenant.CreateRequest{Name: "Genuine Editor isolated fixture"})
	if e != nil {
		t.Fatal(e)
	}
	invite, e := tenants.Invite(ctx, actor, ten.ID, uuid.NewString(), tenant.InviteRequest{Email: foreignEmail, Role: domain.Editor, Locale: "en"})
	if e != nil {
		t.Fatal(e)
	}
	m := mail(`SELECT id,key_id,payload_cipher FROM photo.mail_payloads WHERE message_id=$1`, "<"+invite.ID.String()+"@photoeditor.local>")
	member, e := tenants.AcceptInvite(ctx, foreign, tenant.InviteAccept{Token: m["token"]})
	if e != nil {
		t.Fatal(e)
	}
	if member.TenantID != ten.ID || member.UserID != foreign.UserID || member.Role != domain.Editor {
		t.Fatal("actual membership mismatch")
	}
	projects := &project.Service{DB: apiDB}
	p, e := projects.Create(ctx, actor, ten.ID, uuid.NewString(), project.Create{Name: "Real source fixture", Visibility: "workspace"})
	if e != nil {
		t.Fatal(e)
	}
	if p.ResourceID == nil {
		t.Fatal("missing actual project receipt")
	}
	styles := &style.Service{DB: apiDB}
	s, e := styles.Create(ctx, actor, ten.ID, uuid.NewString(), style.Create{ProjectID: *p.ResourceID, Name: "Pending real imported style"})
	if e != nil {
		t.Fatal(e)
	}
	if s.ResourceID == nil {
		t.Fatal("missing actual style receipt")
	}
	body, e := json.Marshal(struct {
		Tenant, Project, Actor, Session, ForeignActor, ForeignSession, Style uuid.UUID
		StyleVersionReady                                                    bool
	}{ten.ID, *p.ResourceID, actor.UserID, actor.SessionID, foreign.UserID, foreign.SessionID, *s.ResourceID, false})
	if e != nil {
		t.Fatal(e)
	}
	f, e := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if _, e = f.Write(body); e != nil {
		t.Fatal(e)
	}
	if e = f.Sync(); e != nil {
		t.Fatal(e)
	}
}
