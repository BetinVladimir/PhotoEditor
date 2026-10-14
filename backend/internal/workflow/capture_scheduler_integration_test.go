package workflow

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
	"time"
)

func TestIntegrationCaptureStartupRequiresQualifiedOperations(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var qualified bool
	if err = db.QueryRow(ctx, `SELECT photo.capture_operations_ready('cuda')`).Scan(&qualified); err != nil {
		t.Fatal(err)
	}
	if qualified {
		t.Skip("fixture operations already qualified; qualification-block scenario unavailable")
	}
	ready, err := captureSQLReady(ctx, db, "cuda")
	if err != nil {
		t.Fatal(err)
	}
	if ready {
		t.Fatal("capture startup accepted unqualified operations")
	}
}

// All registry/DDL changes are transaction-local and rolled back. This tests
// startup mechanics, not imaging-model qualification.
func TestIntegrationCaptureStartupExactFunctions(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `UPDATE photo.operation_registry SET v2_enabled=true WHERE operation IN('capture.prepare','capture.assess') AND version='1'`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `SET LOCAL ROLE photo_scheduler`); err != nil {
		t.Fatal(err)
	}
	ready, err := captureSQLReady(ctx, tx, "cuda")
	if err != nil || !ready {
		t.Fatalf("positive startup ready=%v err=%v", ready, err)
	}
	ready, err = captureSQLReady(ctx, tx, "mps")
	if err != nil || !ready {
		t.Fatalf("positive Metal startup ready=%v err=%v", ready, err)
	}
	if _, err = tx.Exec(ctx, `RESET ROLE`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `ALTER FUNCTION photo.claim_capture_assessment() RENAME TO capture_startup_missing_probe`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `SET LOCAL ROLE photo_scheduler`); err != nil {
		t.Fatal(err)
	}
	ready, err = captureSQLReady(ctx, tx, "cuda")
	if err != nil || ready {
		t.Fatalf("missing function accepted ready=%v err=%v", ready, err)
	}
	if _, err = tx.Exec(ctx, `RESET ROLE`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `ALTER FUNCTION photo.capture_startup_missing_probe() RENAME TO claim_capture_assessment`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `REVOKE EXECUTE ON FUNCTION photo.claim_capture_assessment() FROM photo_scheduler`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `SET LOCAL ROLE photo_scheduler`); err != nil {
		t.Fatal(err)
	}
	ready, err = captureSQLReady(ctx, tx, "cuda")
	if err != nil || ready {
		t.Fatalf("missing execute accepted ready=%v err=%v", ready, err)
	}
}
