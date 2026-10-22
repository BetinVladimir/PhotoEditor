package run

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/maintenance"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/styleupload"
	"photoeditor/backend/internal/tenant"
)

// The outer native owner must supply a NEW isolated database for EACH named root.
// This reuses the actual OTP/invite/project provisioning implementation unchanged.
// No fixture IDs, ready artifacts, deletion jobs or cleanup tokens are inserted.
type styleOwnedSource struct {
	Path, SHA256 string
	Objects      objectstore.Config
}

func styleOwnedInput(t *testing.T) (styleOwnedSource, string) {
	t.Helper()
	raw, dir := os.Getenv("PHOTO_EDITOR_STYLE_OWNED_SOURCE"), os.Getenv("PHOTO_EDITOR_STYLE_OWNED_OUTPUT")
	if raw == "" && dir == "" {
		t.Skip("owned genuine Style package source not configured")
	}
	if raw == "" || dir == "" || os.Getenv("PHOTO_TEST_DATABASE_URL") == "" || os.Getenv("PHOTO_EDITOR_APPLICATION_CREDENTIALS") == "" || os.Getenv("PHOTO_EDITOR_STYLE_HOSTILE_OWNER_DSN") == "" || os.Getenv("PHOTO_EDITOR_STYLE_MAINTENANCE_CREDENTIAL") == "" {
		t.Fatal("partial owned genuine fixture")
	}
	var f styleOwnedSource
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&f) != nil || d.Decode(new(any)) != io.EOF || !filepath.IsAbs(f.Path) || !filepath.IsAbs(dir) || f.Objects.Bucket == "" {
		t.Fatal("closed real source and owned output required")
	}
	info, e := os.Lstat(dir)
	if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("precreated owned regular output directory")
	}
	return f, dir
}
func styleIssueActor(t *testing.T, dir, label string) styleHostileActor {
	t.Helper()
	path := filepath.Join(dir, "principals-"+label+".private.json")
	ok := t.Run("issue-"+label, func(t *testing.T) {
		t.Setenv("PHOTO_EDITOR_PRINCIPALS_OUTPUT", path)
		TestIntegrationProvisionGenuineEditorPrincipals(t)
	})
	if !ok {
		t.Fatal("actual principal issuance failed")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal("actual principal receipt")
	}
	var p struct {
		Tenant, Project, Actor, Session, ForeignActor, ForeignSession, Style uuid.UUID
		StyleVersionReady                                                    bool
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF || p.Tenant == uuid.Nil || p.Project == uuid.Nil || p.Actor == uuid.Nil || p.Session == uuid.Nil || p.StyleVersionReady {
		t.Fatal("actual unaccepted principal receipt")
	}
	return styleHostileActor{Tenant: p.Tenant, Project: p.Project, Actor: p.Actor, Session: p.Session}
}
func styleIssuePair(t *testing.T) (styleHostileFixture, string) {
	t.Helper()
	s, dir := styleOwnedInput(t)
	f := styleHostileFixture{A: styleIssueActor(t, dir, "a"), B: styleIssueActor(t, dir, "b"), Objects: s.Objects, Path: s.Path, SHA256: s.SHA256}
	if f.A.Tenant == f.B.Tenant || f.A.Project == f.B.Project {
		t.Fatal("distinct genuine tenant receipts required")
	}
	raw, e := json.Marshal(f)
	if e != nil {
		t.Fatal(e)
	}
	// Existing strict helper rechecks a clean database before any package creation.
	t.Setenv("PHOTO_EDITOR_STYLE_HOSTILES", string(raw))
	genuineStyleWrite(t, filepath.Join(dir, "hostile-fixture.private.json"), f)
	return f, dir
}
func TestIntegrationOwnedStylePackageCompleteAbortRace(t *testing.T) {
	styleIssuePair(t)
	TestIntegrationStylePackageCompleteAbortRace(t)
}
func TestIntegrationOwnedStylePackageCleanupTenantIsolation(t *testing.T) {
	styleIssuePair(t)
	TestIntegrationStylePackageCleanupTenantIsolation(t)
}
func TestIntegrationOwnedStylePackageUnknownCreateRecovery(t *testing.T) {
	styleIssuePair(t)
	TestIntegrationStylePackageUnknownCreateRecovery(t)
}

// This issuer performs actual package publication and actual public deletion
// admission, then takes maintenance-issued fenced tickets. It NEVER calls Finish:
// the separately reviewed rollback-only R5 suite consumes the private handoff.
func TestIntegrationIssueStyleDeletionTickets(t *testing.T) {
	f, dir := styleIssuePair(t)
	other := styleIssueActor(t, dir, "other")
	ctx, f, s, objects, maint, audit := styleHostileInputs(t)
	publish := func(actor styleHostileActor) styleupload.Session {
		scoped := f
		scoped.A = actor
		x := styleBegin(t, ctx, scoped, s, actor)
		parts := stylePut(t, ctx, scoped, s, x)
		x, e := s.Complete(ctx, styleWho(actor), actor.Tenant, actor.Project, x.ID, uuid.NewString(), styleupload.CompleteRequest{Parts: parts, ExpectedSize: x.Bytes})
		if e != nil || x.Artifact == nil || x.State != "ready" {
			t.Fatal("actual physical ready package required", e)
		}
		return x
	}
	ready := publish(f.A)
	publish(f.B) // Both rollback targets really have artifacts.
	// Wait the ACTUAL signed URL horizon; no test clock or ledger mutation.
	var until time.Time
	if e := audit.QueryRow(ctx, `SELECT max(signed_until) FROM photo.style_package_uploads WHERE tenant_id IN($1,$2)`, f.A.Tenant, f.B.Tenant).Scan(&until); e != nil {
		t.Fatal("actual signing horizon", e)
	}
	wait := time.Until(until.Add(time.Second))
	if wait > 700*time.Second {
		t.Fatal("unexpected real signing horizon")
	}
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	pending := styleBegin(t, ctx, f, s, f.B)
	if _, e := s.Abort(ctx, styleWho(f.B), f.B.Tenant, f.B.Project, pending.ID); e != nil {
		t.Fatal("actual pending abort", e)
	}
	api := genuineDedicatedPool(t, ctx, "api")
	tenants := tenant.Service{DB: api}
	jobs := map[uuid.UUID]uuid.UUID{}
	for _, a := range []styleHostileActor{f.A, f.B} {
		current, e := tenants.Get(ctx, auth.Identity{UserID: a.Actor, SessionID: a.Session}, a.Tenant)
		if e != nil {
			t.Fatal("current tenant read", e)
		}
		revision, e := strconv.ParseInt(current.Revision, 10, 64)
		if e != nil {
			t.Fatal("actual revision", e)
		}
		accepted, e := tenants.Delete(ctx, styleWho(a), a.Tenant, revision)
		if e != nil || accepted.JobID == uuid.Nil {
			t.Fatal("actual deletion API receipt", e)
		}
		jobs[a.Tenant] = accepted.JobID
	}
	repo := maintenance.SQLTenantDeletion{DB: maint}
	tickets := map[uuid.UUID]maintenance.TenantDeletion{}
	for i := 0; i < 2; i++ {
		ticket, e := repo.Claim(ctx)
		if e != nil || ticket == nil || jobs[ticket.TenantID] != ticket.JobID || ticket.Token == uuid.Nil {
			t.Fatal("actual owned deletion claim", e)
		}
		if _, duplicate := tickets[ticket.TenantID]; duplicate {
			t.Fatal("duplicate tenant claim")
		}
		tickets[ticket.TenantID] = *ticket
	}
	// Real empty physical prefix is a prerequisite of Finish. The pending ledger
	// remains genuinely aborting; deleting its objects does not forge SQL cleanup.
	for _, ticket := range tickets {
		for pass := 0; ; pass++ {
			if pass >= 32 {
				t.Fatal("bounded physical purge")
			}
			for _, live := range tickets {
				ok, e := repo.RenewLease(ctx, live)
				if e != nil || !ok {
					t.Fatal("actual deletion lease", e)
				}
			}
			bounded, stop := context.WithTimeout(ctx, 40*time.Second)
			more, e := objects.PurgeTenantBatch(bounded, ticket.TenantID, 256)
			timed := bounded.Err()
			stop()
			if e != nil || timed != nil {
				t.Fatal("physical tenant purge", e)
			}
			if !more {
				break
			}
		}
		state, e := repo.SettleArchiveGenerations(ctx, ticket)
		if e != nil || state != "complete" {
			t.Fatal("actual archive settlement", e)
		}
		for _, live := range tickets {
			ok, e := repo.RenewLease(ctx, live)
			if e != nil || !ok {
				t.Fatal("verification lease", e)
			}
		}
		verify, stopVerify := context.WithTimeout(ctx, 40*time.Second)
		more, e := objects.PurgeTenantBatch(verify, ticket.TenantID, 256)
		timed := verify.Err()
		stopVerify()
		if e != nil || timed != nil || more {
			t.Fatal("second physical empty-prefix verification", e)
		}
	}
	a, b := tickets[f.A.Tenant], tickets[f.B.Tenant]
	for _, ticket := range []maintenance.TenantDeletion{a, b} {
		ok, e := repo.RenewLease(ctx, ticket)
		if e != nil || !ok {
			t.Fatal("handoff lease", e)
		}
	}
	genuineStyleWrite(t, filepath.Join(dir, "deletion-fixture.private.json"), map[string]any{"tenant": f.A.Tenant, "other_tenant": other.Tenant, "ready_job": a.JobID, "ready_token": a.Token, "pending_job": b.JobID, "pending_token": b.Token, "pending_upload": pending.ID, "ready_upload": ready.ID, "pending_tenant": f.B.Tenant})
	genuineStyleWrite(t, filepath.Join(dir, "deletion-handoff.json"), map[string]any{"schema_version": 1, "api_created": true, "finish_called": false, "lease_seconds": 60, "requires_immediate_renewal": true, "package_bucket": f.Objects.Bucket})
}
