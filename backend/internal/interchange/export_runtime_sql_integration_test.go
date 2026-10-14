package interchange

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestExportRuntimeAdmissionUsesCurrentBucketAndHealthyLease(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var definition string
	err = db.QueryRow(ctx, `SELECT pg_get_functiondef('photo.v30_interchange_export_draft_ready(uuid,uuid,uuid,text,text,text)'::regprocedure)`).Scan(&definition)
	if err != nil || !strings.Contains(definition, "v30_export_runtime_ready((SELECT a.bucket FROM photo.artifacts") {
		t.Fatal("export draft admission lacks bucket-bound runtime", err)
	}
	err = db.QueryRow(ctx, `SELECT pg_get_functiondef('photo.v30_export_pending_runtime_ready(uuid,uuid,uuid,text,text)'::regprocedure)`).Scan(&definition)
	if err != nil || !strings.Contains(definition, "v30_interchange_export_document_scope") ||
		!strings.Contains(definition, "v30_export_runtime_ready(bucket)") {
		t.Fatal("export runtime diagnostic lacks current pending authority", err)
	}
	var ownerDelete, apiRead, schedulerWrite bool
	err = db.QueryRow(ctx, `SELECT
  has_table_privilege('photo_fn_owner','photo.v30_export_runtime_leases','DELETE'),
  has_table_privilege('photo_api','photo.v30_export_runtime_leases','SELECT'),
  has_table_privilege('photo_scheduler','photo.v30_export_runtime_leases','UPDATE')`).Scan(
		&ownerDelete, &apiRead, &schedulerWrite)
	if err != nil || ownerDelete || apiRead || schedulerWrite {
		t.Fatal("export runtime lease table ACL drift", err)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	instance := uuid.New()
	var ready, accepted bool
	if _, err = tx.Exec(ctx, "SET LOCAL ROLE photo_api"); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `SELECT photo.v30_export_runtime_ready('export-bucket')`).Scan(&ready); err != nil || ready {
		t.Fatal("absent export scheduler admitted", err, ready)
	}
	var unknown *bool
	if err = tx.QueryRow(ctx, `SELECT photo.v30_export_pending_runtime_ready($1,$2,$3,'missing', $4)`,
		uuid.New(), uuid.New(), uuid.New(), strings.Repeat("a", 64)).Scan(&unknown); err != nil || unknown != nil {
		t.Fatal("fabricated export pending scope disclosed runtime", err, unknown)
	}
	if _, err = tx.Exec(ctx, "SET LOCAL ROLE photo_scheduler"); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `SELECT photo.announce_v30_export_runtime($1,'export-bucket')`, instance).Scan(&accepted); err != nil || !accepted {
		t.Fatal("export runtime announcement refused", err, accepted)
	}
	if _, err = tx.Exec(ctx, "SET LOCAL ROLE photo_api"); err != nil {
		t.Fatal(err)
	}
	for bucket, expected := range map[string]bool{"export-bucket": true, "other-bucket": false} {
		if err = tx.QueryRow(ctx, `SELECT photo.v30_export_runtime_ready($1)`, bucket).Scan(&ready); err != nil || ready != expected {
			t.Fatal("export runtime bucket gate", bucket, err, ready)
		}
	}
	if _, err = tx.Exec(ctx, "SET LOCAL ROLE photo_scheduler"); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `SELECT photo.withdraw_v30_export_runtime($1)`, instance); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "SET LOCAL ROLE photo_api"); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `SELECT photo.v30_export_runtime_ready('export-bucket')`).Scan(&ready); err != nil || ready {
		t.Fatal("withdrawn export scheduler admitted", err, ready)
	}
}
