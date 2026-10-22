package billing_test

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/billing"
	"photoeditor/backend/internal/domain"
	runservice "photoeditor/backend/internal/run"
	"testing"
	"time"
)

// Invoked by Python with an isolated migrated DB and authentic actor/session.
func TestIntegrationOperationColdAdmission(t *testing.T) {
	raw := os.Getenv("PHOTO_BILLING_OPERATION_FIXTURE")
	if raw == "" {
		t.Skip("isolated operation fixture required")
	}
	var f struct {
		Tenant, Run, Actor, Session uuid.UUID
		Epoch                       int64
	}
	if json.Unmarshal([]byte(raw), &f) != nil {
		t.Fatal("fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	cfg, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	api, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	// Raw SQL tenant fixture bypasses normal tenant provisioning.
	// Keep native domain-first locking enabled; supply its required usage row.
	if _, err = admin.Exec(ctx, `INSERT INTO photo.tenant_usage(tenant_id) VALUES($1) ON CONFLICT(tenant_id) DO NOTHING`, f.Tenant); err != nil {
		t.Fatal("tenant usage fixture", err)
	}
	blocker, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(ctx)
	if _, err = blocker.Exec(ctx, `SELECT id FROM photo.tenants WHERE id=$1 FOR UPDATE`, f.Tenant); err != nil {
		t.Fatal(err)
	}
	tx, err := api.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	err = billing.PrepareOperations(ctx, tx, f.Tenant, f.Run)
	tx.Rollback(ctx)
	if domain.AsError(err).Code != "BILLING_UNAVAILABLE" || time.Since(started) > time.Second {
		t.Fatal("cold lock gate", err, time.Since(started))
	}
	blocker.Rollback(ctx)
	tx, err = api.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err = billing.PrepareOperations(ctx, tx, f.Tenant, f.Run); err != nil {
		t.Fatal("fresh preflight", err)
	}
	// Real seal wrapper enforces pricing, then rolls all accounting back when the
	// original graph authority refuses the intentionally absent transport closure.
	var sealed bool
	if err = tx.QueryRow(ctx, `SELECT photo.seal_v2_graph($1,$2)`, f.Tenant, f.Run).Scan(&sealed); err != nil || sealed {
		t.Fatal("legacy seal refusal", sealed, err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = admin.QueryRow(ctx, `SELECT count(*) FROM photo.billing_operation_requests WHERE tenant_id=$1`, f.Tenant).Scan(&count); err != nil || count != 0 {
		t.Fatal("refused seal leaked accounting", count, err)
	}
	// Native user cancellation drains accepted operation funding exactly once.
	if _, err = admin.Exec(ctx, `SELECT photo.billing_bind_operations($1,$2)`, f.Tenant, f.Run); err != nil {
		t.Fatal("accepted funding", err)
	}
	if _, err = admin.Exec(ctx, `UPDATE photo.runs SET status='queued',graph_sealed=true WHERE id=$1`, f.Run); err != nil {
		t.Fatal(err)
	}
	userCfg, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	userCfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	userDB, err := pgxpool.NewWithConfig(ctx, userCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer userDB.Close()
	svc := runservice.Service{DB: userDB}
	who := auth.Identity{UserID: f.Actor, SessionID: f.Session, Epoch: f.Epoch}
	key := uuid.NewString()
	if _, err = svc.Cancel(ctx, who, f.Tenant, f.Run, key); err != nil {
		t.Fatal("native cancellation", err)
	}
	if _, err = svc.Cancel(ctx, who, f.Tenant, f.Run, key); err != nil {
		t.Fatal("native cancel replay", err)
	}
	for i := 0; i < 4; i++ {
		if _, err = admin.Exec(ctx, `SELECT photo.cancel_idle_task()`); err != nil {
			t.Fatal(err)
		}
		if _, err = admin.Exec(ctx, `SELECT photo.finalize_run_cancellation()`); err != nil {
			t.Fatal(err)
		}
	}
	var changed bool
	if err = api.QueryRow(ctx, `SELECT photo.billing_settle_next_operations()`).Scan(&changed); err != nil || !changed {
		t.Fatal("cancel settlement", changed, err)
	}
	if err = api.QueryRow(ctx, `SELECT photo.billing_settle_next_operations()`).Scan(&changed); err != nil || changed {
		t.Fatal("duplicate refund", changed, err)
	}
	var state string
	if err = admin.QueryRow(ctx, `SELECT state FROM photo.billing_operation_items WHERE tenant_id=$1`, f.Tenant).Scan(&state); err != nil || state != "released" {
		t.Fatal("funding state", state, err)
	}

}
