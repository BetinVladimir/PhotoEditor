package workflow

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/objectstore"
	"testing"
	"time"
)

func TestIntegrationRenderRecoveryRuntimeLoop(t *testing.T) {
	raw := os.Getenv("PHOTO_RENDER_OBJECT_CONFIG")
	if raw == "" {
		t.Skip("isolated versioned object store and SQL fixture required")
	}
	ctx, stop := context.WithTimeout(context.Background(), 30*time.Second)
	defer stop()
	var config objectstore.Config
	if json.Unmarshal([]byte(raw), &config) != nil {
		t.Fatal("invalid object fixture configuration")
	}
	objects, err := objectstore.New(config)
	if err != nil {
		t.Fatal("object fixture unavailable")
	}
	cfg, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal("invalid database fixture")
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("scheduler fixture unavailable")
	}
	defer pool.Close()
	job, closeSources, err := NewRenderRecoveryProcessor(ctx, pool, os.Getenv("PHOTO_TEST_DATABASE_URL"), objects, config.Bucket)
	if err != nil {
		t.Fatal("construct mounted recovery processor", err)
	}
	defer closeSources()
	checkpointRaw, err := os.ReadFile(os.Getenv("PHOTO_RENDER_TYPED_FIXTURE") + ".runtime")
	if err != nil {
		t.Fatal(err)
	}
	var checkpoint struct {
		Tenant, Batch uuid.UUID
		Tasks         []uuid.UUID
	}
	if json.Unmarshal(checkpointRaw, &checkpoint) != nil || len(checkpoint.Tasks) != 2 {
		t.Fatal("invalid runtime checkpoint")
	}
	admin, err := pgxpool.New(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal("database fixture unavailable")
	}
	defer admin.Close()
	var registered int
	if err := admin.QueryRow(ctx, "SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=$1 AND task_id=ANY($2::uuid[])", checkpoint.Tenant, checkpoint.Tasks).Scan(&registered); err != nil || registered != 0 {
		t.Fatal("runtime fixture was already published", err, registered)
	}
	var recipe uuid.UUID
	if err := admin.QueryRow(ctx, "SELECT params_artifact_id FROM photo.tasks WHERE tenant_id=$1 AND id=$2", checkpoint.Tenant, checkpoint.Tasks[1]).Scan(&recipe); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND id=$2", checkpoint.Tenant, recipe); err != nil {
		t.Fatal(err)
	}
	if changed, err := job.ProcessOne(ctx); err != nil || changed {
		t.Fatal("rejected input affected scheduler readiness", err, changed)
	}
	var failures int
	var delayed bool
	if err := admin.QueryRow(ctx, "SELECT v26_input_recovery_failures,v26_input_recovery_after>clock_timestamp() FROM photo.runs WHERE id=$1", checkpoint.Batch).Scan(&failures, &delayed); err != nil || failures != 1 || !delayed {
		t.Fatal("recovery backoff not persisted", err, failures, delayed)
	}
	// New runtime/pool simulates losing all in-memory cursor state.
	closeSources()
	job, closeSources, err = NewRenderRecoveryProcessor(ctx, pool, os.Getenv("PHOTO_TEST_DATABASE_URL"), objects, config.Bucket)
	if err != nil {
		t.Fatal(err)
	}
	defer closeSources()
	if changed, err := job.ProcessOne(ctx); err != nil || changed {
		t.Fatal("restart bypassed persisted backoff", err, changed)
	}
	if _, err := admin.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE tenant_id=$1 AND id=$2", checkpoint.Tenant, recipe); err != nil {
		t.Fatal(err)
	}
	// DBA fixture advances only the due timestamp; guards remain enabled.
	tx, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_fn_owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "UPDATE photo.runs SET v26_input_recovery_after=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND id=$2", checkpoint.Tenant, checkpoint.Batch); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	state := new(ExpansionLoopState)
	loop, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RunExpansionLoop(loop, job, state, 10*time.Millisecond) }()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for !state.Ready.Load() || state.Materialized.Load() == 0 {
		select {
		case <-ctx.Done():
			t.Fatal("recovery loop did not become ready")
		case <-ticker.C:
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("recovery loop did not stop")
	}
	if state.Ready.Load() || state.Failures.Load() != 0 || state.Materialized.Load() != 1 {
		t.Fatal("active recovery lifecycle counters invalid")
	}
	if err := admin.QueryRow(ctx, "SELECT v26_input_recovery_failures,v26_input_recovery_after IS NULL FROM photo.runs WHERE id=$1", checkpoint.Batch).Scan(&failures, &delayed); err != nil || failures != 0 || !delayed {
		t.Fatal("successful recovery did not reset backoff", err, failures, delayed)
	}
	var sealed, blocked int
	if err := admin.QueryRow(ctx, `SELECT count(*) FILTER(WHERE photo.v2_input_closure_ready(id)),count(*) FILTER(WHERE state='blocked' AND attempt=0) FROM photo.tasks WHERE tenant_id=$1 AND id=ANY($2::uuid[])`, checkpoint.Tenant, checkpoint.Tasks).Scan(&sealed, &blocked); err != nil || sealed != 2 || blocked != 2 {
		t.Fatal("runtime did not seal inputs without activation", err, sealed, blocked)
	}

}
