package run

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The same actual accepted immutable origin used by the existing replay test
// supplies this protocol test. It neither creates a fake accepted build nor
// asserts model/render qualification.
func TestIntegrationEditorIdentityLockAfterSessionWait(t *testing.T) {
	dsn, raw := os.Getenv("PHOTO_TEST_DATABASE_URL"), os.Getenv("PHOTO_TEST_EDITOR_ACCEPTED_FIXTURE")
	if dsn == "" || raw == "" {
		t.Skip("isolated migrated DB and actual accepted Editor fixture required")
	}
	var f struct {
		Tenant, Project, Actor, Session uuid.UUID
		Request                         EditorEffectRequest
	}
	if json.Unmarshal([]byte(raw), &f) != nil || f.Tenant == uuid.Nil || f.Project == uuid.Nil || f.Actor == uuid.Nil || f.Session == uuid.Nil {
		t.Fatal("invalid actual editor fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	owner, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer owner.Close()
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer api.Close()
	const query = `SELECT photo.lock_editor_effect_identity($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`
	args := []any{f.Tenant, f.Project, f.Request.Source.AssetVersionID, f.Request.Source.Source.ArtifactID, f.Request.Source.Source.SHA256, f.Request.Recipe.ArtifactID, f.Request.Recipe.SHA256, f.Request.FrozenDocumentID, f.Request.DraftDocumentID, f.Request.DraftRevision, f.Actor, f.Session}
	check := func(want bool) {
		t.Helper()
		tx, e := api.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback(context.Background())
		if _, e = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", f.Tenant.String()); e != nil {
			t.Fatal(e)
		}
		var got bool
		if e = tx.QueryRow(ctx, query, args...).Scan(&got); e != nil || got != want {
			t.Fatal("exact bound lock result", got, want, e)
		}
	}
	var usersRead, sessionsRead bool
	if e = owner.QueryRow(ctx, `SELECT has_table_privilege('photo_api','photo.users','SELECT'),has_table_privilege('photo_api','photo.sessions','SELECT')`).Scan(&usersRead, &sessionsRead); e != nil || usersRead || sessionsRead {
		t.Fatal("global identity table privacy", e)
	}
	check(true)
	holding, e := owner.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer holding.Rollback(context.Background())
	var old *time.Time
	if e = holding.QueryRow(ctx, "SELECT revoked_at FROM photo.sessions WHERE id=$1 AND user_id=$2 FOR UPDATE", f.Session, f.Actor).Scan(&old); e != nil || old != nil {
		t.Fatal("live actual session", e)
	}
	// The only committed mutation is restored exactly, even on assertion failure.
	changed := false
	defer func() {
		if changed {
			restoreCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
			defer c()
			if _, err := owner.Exec(restoreCtx, "UPDATE photo.sessions SET revoked_at=$2 WHERE id=$1 AND user_id=$3", f.Session, old, f.Actor); err != nil {
				t.Error("restore actual session", err)
			}
		}
	}()
	waiting, e := api.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer waiting.Rollback(context.Background())
	if _, e = waiting.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", f.Tenant.String()); e != nil {
		t.Fatal(e)
	}
	var pid int
	if e = waiting.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); e != nil {
		t.Fatal(e)
	}
	type result struct {
		allowed bool
		err     error
	}
	done := make(chan result, 1)
	go func() {
		var allowed bool
		err := waiting.QueryRow(ctx, query, args...).Scan(&allowed)
		done <- result{allowed, err}
	}()
	blocked := false
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		if e = owner.QueryRow(ctx, "SELECT cardinality(pg_blocking_pids($1))>0", pid).Scan(&blocked); e != nil {
			t.Fatal(e)
		}
		if blocked {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !blocked {
		t.Fatal("actual bound lock did not wait for session writer")
	}
	if _, e = holding.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1 AND user_id=$2", f.Session, f.Actor); e != nil {
		t.Fatal(e)
	}
	if e = holding.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	changed = true
	select {
	case r := <-done:
		if r.err != nil || r.allowed {
			t.Fatal("revocation after actual lock wait accepted", r.err)
		}
	case <-ctx.Done():
		t.Fatal("bounded session wait", ctx.Err())
	}
	if e = waiting.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=$2 WHERE id=$1 AND user_id=$3", f.Session, old, f.Actor); e != nil {
		t.Fatal(e)
	}
	changed = false
	check(true)
}
