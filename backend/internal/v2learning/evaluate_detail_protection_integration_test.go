package v2learning

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The fixture is a live, blocked Evaluate task with a sealed input closure,
// succeeded Fit parent and one linked portrait.detail holdout document. Every
// mutation is rolled back; the test never closes or activates the task.
func TestIntegrationEvaluateDetailProtectionBinding(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	parts := strings.Split(os.Getenv("PHOTO_TEST_V23_DETAIL_EVALUATE_IDS"), ",")
	if dsn == "" || len(parts) != 5 {
		t.Skip("isolated V23 detail Evaluate fixture required")
	}
	ids := make([]uuid.UUID, len(parts))
	for i, part := range parts {
		var err error
		ids[i], err = uuid.Parse(strings.TrimSpace(part))
		if err != nil || ids[i] == uuid.Nil {
			t.Fatalf("invalid V23 detail fixture id %d: %q", i, part)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
	defer tx.Rollback(ctx)

	current := func() bool {
		t.Helper()
		var ready bool
		if err := tx.QueryRow(ctx, `SELECT photo.v23_evaluate_detail_protection_current($1,$2,$3,$4,$5)`,
			ids[0], ids[1], ids[2], ids[3], ids[4]).Scan(&ready); err != nil {
			t.Fatal(err)
		}
		return ready
	}
	if !current() {
		t.Fatal("valid fixture has no exact protection binding")
	}
	var documentID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT d.id FROM photo.backend_documents d
 JOIN photo.v2_task_inputs i ON i.tenant_id=d.tenant_id AND
  i.artifact_id=d.artifact_id AND i.sha256=d.sha256
 WHERE d.tenant_id=$1 AND d.run_id=$2 AND i.task_id=$3 AND
  d.purpose='v23_detail_holdout' AND
  i.contract='FrozenPortraitDetailHoldoutEvidence'
 ORDER BY d.id LIMIT 1`, ids[0], ids[1], ids[4]).Scan(&documentID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "SAVEPOINT protection_control"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE photo.backend_documents SET
 v23_detail_protection_id=NULL,v23_detail_protection_sha=NULL WHERE id=$1`,
		documentID); err != nil {
		t.Fatal(err)
	}
	if current() {
		t.Fatal("missing protection binding admitted")
	}
	if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT protection_control"); err != nil {
		t.Fatal(err)
	}
	if !current() {
		t.Fatal("restored protection binding rejected")
	}
	if _, err := tx.Exec(ctx, `UPDATE photo.backend_documents SET
 v23_detail_protection_sha=CASE WHEN v23_detail_protection_sha=repeat('0',64)
  THEN repeat('1',64) ELSE repeat('0',64) END WHERE id=$1`,
		documentID); err != nil {
		t.Fatal(err)
	}
	if current() {
		t.Fatal("substituted protection SHA admitted")
	}
}
