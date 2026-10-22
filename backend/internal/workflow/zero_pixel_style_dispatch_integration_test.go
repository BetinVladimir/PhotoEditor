package workflow

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/database"
	"testing"
	"time"
)

func TestIntegrationDispatcherUsesExistingSealedZeroPixelStyleProfile(t *testing.T) {
	if os.Getenv("PHOTO_ZERO_PIXEL_OWNER_DSN") == "" {
		t.Skip("isolated actual style fixture required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	owner, e := pgxpool.New(ctx, os.Getenv("PHOTO_ZERO_PIXEL_OWNER_DSN"))
	if e != nil {
		t.Fatal("owner config")
	}
	defer owner.Close()
	var db string
	if e = owner.QueryRow(ctx, "select current_database()").Scan(&db); e != nil || db != "photoeditor_style_claim_r3" {
		t.Fatal("isolated fixture refused")
	}
	var task uuid.UUID
	var before string
	var pixels int64
	var cap int64
	if e = owner.QueryRow(ctx, `select id,execution_profile::text,(execution_profile->'resource_estimate'->>'pixel_count')::bigint,(execution_profile->>'max_pixels')::bigint from photo.tasks where operation='style.import'and state='ready'`).Scan(&task, &before, &pixels, &cap); e != nil || pixels != 0 || cap != 0 {
		t.Fatal("actual immutable fixture missing")
	}
	// The retained owned template also contains exactly three original upload
	// regression verify tasks. Include only those explicit fixture run bindings;
	// no qualified worker may exist for any candidate in this isolated test.
	allowedVerify := map[string]bool{"773a1d22-7a73-48c9-87c3-5c074a3e3b89": true, "9a5412be-9c5b-46af-b856-088c27c98023": true, "fe660ac6-c8ad-4fe5-a1d2-f41ba37b29bf": true}
	rows, e := owner.Query(ctx, "select id,run_id,operation,state from photo.tasks where state in('ready','queued','running')")
	if e != nil {
		t.Fatal("fixture inventory")
	}
	var candidates []uuid.UUID
	for rows.Next() {
		var id, run uuid.UUID
		var op, state string
		if e = rows.Scan(&id, &run, &op, &state); e != nil {
			t.Fatal("fixture row")
		}
		if state != "ready" || (id != task && (!allowedVerify[run.String()] || op != "asset.verify")) {
			t.Fatal("outside owned four-task fixture")
		}
		candidates = append(candidates, id)
	}
	rows.Close()
	if len(candidates) != 4 {
		t.Fatal("exact owned fixture inventory drift")
	}
	cfg, e := pgxpool.ParseConfig(os.Getenv("PHOTO_ZERO_PIXEL_SCHEDULER_DSN"))
	if e != nil {
		t.Fatal("scheduler config")
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	database.GuardApplicationRole(cfg)
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal("scheduler pool")
	}
	defer pool.Close()
	if e = database.RequireApplicationRole(ctx, pool, "photo_scheduler"); e != nil {
		t.Fatal("scheduler authority")
	}
	for _, id := range candidates {
		var eligible int
		if e = pool.QueryRow(ctx, "select count(*)from photo.eligible_dispatch_nodes($1)", id).Scan(&eligible); e != nil || eligible != 0 {
			t.Fatal("fixture must have no qualified live worker; no task delivery intended")
		}
	}
	if _, e = ParseResourceEstimate([]byte(before)); e != nil {
		t.Fatal("existing generated profile rejected")
	}
	n, e := (Dispatcher{DB: pool}).Dispatch(ctx, 20)
	if e != nil || n != 0 {
		t.Fatal("actual normal dispatcher failed or unexpectedly delivered")
	}
	var state, reason, after string
	if e = owner.QueryRow(ctx, "select state,terminal_reason,execution_profile::text from photo.tasks where id=$1", task).Scan(&state, &reason, &after); e != nil || state != "ready" || reason != "waiting_worker" || after != before {
		t.Fatal("normal admission did not preserve immutable profile or reached invalid estimate")
	}
	t.Log("ACTUAL_EXISTING_ZERO_PIXEL_STYLE_TASK_NORMAL_DISPATCH_WAITING_WORKER_PROFILE_UNCHANGED_PASS")
}
