package delivery

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMigratedSignedFailureRequiresCurrentParentLease(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" { t.Skip("isolated migrated PostgreSQL required") }
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil { t.Fatal(err) }
	defer db.Close()
	var accepted bool
	if err := db.QueryRow(ctx,
		`SELECT photo.fail_v31_delivery_signing($1,$2,$3,$4)`,
		uuid.New(), uuid.New(), 0, uuid.New()).Scan(&accepted); err != nil || accepted {
		t.Fatalf("fabricated parent lease finalized signed package: %v", err)
	}
	var scheduler, api, worker bool
	if err := db.QueryRow(ctx, `SELECT
		has_function_privilege('photo_scheduler','photo.fail_v31_delivery_signing(uuid,uuid,integer,uuid)','EXECUTE'),
		has_function_privilege('photo_api','photo.fail_v31_delivery_signing(uuid,uuid,integer,uuid)','EXECUTE'),
		has_function_privilege('photo_worker','photo.fail_v31_delivery_signing(uuid,uuid,integer,uuid)','EXECUTE')`).Scan(&scheduler,&api,&worker); err != nil || !scheduler || api || worker {
		t.Fatalf("signed failure ACL drift: scheduler=%v api=%v worker=%v err=%v", scheduler,api,worker,err)
	}
}
