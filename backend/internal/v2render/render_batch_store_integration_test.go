package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
	"time"
)

func TestIntegrationRenderBatchManifestRecoveryAcrossPhases(t *testing.T) {
	dsn, id := os.Getenv("PHOTO_TEST_DATABASE_URL"), os.Getenv("PHOTO_RENDER_BATCH")
	if dsn == "" || id == "" {
		t.Skip("isolated committed batch fixture required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	batch := uuid.MustParse(id)
	var tenant uuid.UUID
	var who auth.Identity
	if err := admin.QueryRow(ctx, "SELECT tenant_id,created_by,(config_snapshot->>'auth_session_id')::uuid FROM photo.runs WHERE id=$1", batch).Scan(&tenant, &who.UserID, &who.SessionID); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := SQLRenderDraftStore{DB: pool}
	first, err := store.LoadBatch(ctx, tenant, batch, who)
	if err != nil || first.BatchID != batch || len(first.Manifest.Parts) != 1 {
		t.Fatal("initial load", err)
	}
	for _, phase := range []string{"queued", "running", "waiting_review", "failed"} {
		if _, err := admin.Exec(ctx, "UPDATE photo.runs SET status=$2 WHERE id=$1", batch, phase); err != nil {
			t.Fatal(err)
		}
		saved, err := store.LoadBatch(ctx, tenant, batch, who)
		if err != nil || saved.ManifestSHA != first.ManifestSHA || saved.Manifest.Parts[0] != first.Manifest.Parts[0] || saved.CancelRequested {
			t.Fatal("immutable recovery across phase", phase, err)
		}
	}
	if _, err := admin.Exec(ctx, "UPDATE photo.runs SET status='cancelling',cancel_requested=true WHERE id=$1", batch); err != nil {
		t.Fatal(err)
	}
	saved, err := store.LoadBatch(ctx, tenant, batch, who)
	if err != nil || !saved.CancelRequested {
		t.Fatal("cancellation evidence", err)
	}
	foreign := who
	foreign.SessionID = uuid.New()
	if _, err := store.LoadBatch(ctx, tenant, batch, foreign); !errors.Is(err, v2input.ErrExpired) {
		t.Fatal("foreign session", err)
	}
	if _, err := store.LoadBatch(ctx, uuid.New(), batch, who); !errors.Is(err, v2input.ErrExpired) {
		t.Fatal("foreign tenant", err)
	}
	foreign = who
	foreign.UserID = uuid.New()
	if _, err := store.LoadBatch(ctx, tenant, batch, foreign); !errors.Is(err, v2input.ErrExpired) {
		t.Fatal("foreign actor", err)
	}
	if _, err := admin.Exec(ctx, "UPDATE photo.sessions SET revoked_at=now() WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadBatch(ctx, tenant, batch, who); !errors.Is(err, v2input.ErrExpired) {
		t.Fatal("revoked session", err)
	}
}
