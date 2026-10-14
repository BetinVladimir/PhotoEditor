package delivery

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Runs against an isolated database after migrations have been applied.
func TestDeliveryEncodeBackoffRequiresExactTaskProfile(t *testing.T) {
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
	signature := "photo.defer_v30_delivery_encode(uuid,uuid)"
	var definition string
	var scheduler, api, worker, public bool
	err = db.QueryRow(ctx, `SELECT pg_get_functiondef($1::regprocedure),
  has_function_privilege('photo_scheduler',$1,'EXECUTE'),
  has_function_privilege('photo_api',$1,'EXECUTE'),
  has_function_privilege('photo_worker',$1,'EXECUTE'),
  EXISTS(SELECT 1 FROM pg_proc p CROSS JOIN LATERAL aclexplode(p.proacl) a
   WHERE p.oid=$1::regprocedure AND a.grantee=0 AND a.privilege_type='EXECUTE')`,
		signature).Scan(&definition, &scheduler, &api, &worker, &public)
	if err != nil || !scheduler || api || worker || public {
		t.Fatal("encode backoff privilege drift", err)
	}
	// PostgreSQL normalizes casts and whitespace in pg_get_functiondef.
	for _, fragment := range []string{"operation_version IS DISTINCT FROM", "resource_class IS DISTINCT FROM",
		"renderer_version", "IS DISTINCT FROM 'v2.2'"} {
		if !strings.Contains(definition, fragment) {
			t.Fatalf("missing encode profile gate: %s", fragment)
		}
	}
	// The earlier unlocked task lookup only discovers the run ID. Both paths
	// must acquire the run row before locking the task to avoid a deadlock with
	// terminal rejection and adoption in concurrent scheduler transactions.
	runLock := strings.Index(definition, "FROM photo.runs WHERE tenant_id=p_tenant AND id=run_id FOR UPDATE")
	taskLock := strings.Index(definition, "FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE")
	if runLock < 0 || taskLock < 0 || runLock >= taskLock {
		t.Fatal("encode backoff must lock run before task")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SET LOCAL ROLE photo_scheduler"); err != nil {
		t.Fatal(err)
	}
	var deferred bool
	if err = tx.QueryRow(ctx, "SELECT photo.defer_v30_delivery_encode($1,$2)", uuid.New(), uuid.New()).Scan(&deferred); err != nil {
		t.Fatal(err)
	}
	if deferred {
		t.Fatal("absent tenant/task received a backoff receipt")
	}
}
