package delivery

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Requires an isolated database migrated through 02122 and a test owner that
// may SET ROLE. The lease is written only inside a rolled-back transaction.
func TestDeliveryRuntimeBucketLeaseAndRoleBoundary(t *testing.T) {
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
	var aclOK bool
	err = db.QueryRow(ctx, `SELECT
  has_function_privilege('photo_scheduler','photo.announce_v30_delivery_runtime(uuid,text)','EXECUTE')
  AND has_function_privilege('photo_scheduler','photo.withdraw_v30_delivery_runtime(uuid)','EXECUTE')
  AND NOT has_function_privilege('photo_api','photo.announce_v30_delivery_runtime(uuid,text)','EXECUTE')
  AND NOT has_function_privilege('photo_worker','photo.announce_v30_delivery_runtime(uuid,text)','EXECUTE')
  AND has_function_privilege('photo_api','photo.v30_delivery_runtime_ready(text)','EXECUTE')
  AND has_function_privilege('photo_api','photo.begin_v30_delivery_create_run_bound(uuid,uuid,uuid,text,text,text,text)','EXECUTE')
  AND has_function_privilege('photo_api','photo.activate_v30_delivery_create_bound(uuid,uuid,uuid,text,text,text)','EXECUTE')
  AND NOT has_function_privilege('photo_api','photo.begin_v30_delivery_create_run(uuid,uuid,uuid,text,text,text)','EXECUTE')
  AND NOT has_function_privilege('photo_api','photo.activate_v30_delivery_create(uuid,uuid,uuid,text,text)','EXECUTE')`).Scan(&aclOK)
	if err != nil || !aclOK {
		t.Fatal("delivery runtime ABI or grants drifted", err)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	instance := uuid.New()
	if _, err = tx.Exec(ctx, `SET LOCAL ROLE photo_scheduler`); err != nil {
		t.Fatal(err)
	}
	var announced bool
	if err = tx.QueryRow(ctx, `SELECT photo.announce_v30_delivery_runtime($1,$2)`, instance, "photos-a").Scan(&announced); err != nil || !announced {
		t.Fatal("scheduler lease refused", err)
	}
	if _, err = tx.Exec(ctx, `SET LOCAL ROLE photo_api`); err != nil {
		t.Fatal(err)
	}
	var correct, foreign bool
	if err = tx.QueryRow(ctx, `SELECT photo.v30_delivery_runtime_ready('photos-a'),photo.v30_delivery_runtime_ready('photos-b')`).Scan(&correct, &foreign); err != nil || !correct || foreign {
		t.Fatal("bucket-scoped admission changed", correct, foreign, err)
	}
	if _, err = tx.Exec(ctx, `SET LOCAL ROLE photo_scheduler`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `SELECT photo.withdraw_v30_delivery_runtime($1)`, instance); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `SET LOCAL ROLE photo_api`); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `SELECT photo.v30_delivery_runtime_ready('photos-a')`).Scan(&correct); err != nil || correct {
		t.Fatal("withdrawn lease admitted work", err)
	}
}
