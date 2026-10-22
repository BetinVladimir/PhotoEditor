package maintenance

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Authored only. Requires an isolated, fully migrated operator test database;
// all privilege changes roll back before another case begins.
func TestIntegrationCollectionCleanupInstallationChecksEffectivePrivileges(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" { t.Skip("isolated migrated PostgreSQL required") }
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil { t.Fatal(err) }
	defer db.Close()
	for _, scenario := range []struct{name, mutation string; ready bool}{
		{"installed", "", true},
		{"missing_claim_privilege", "REVOKE EXECUTE ON FUNCTION photo.claim_collection_deletion() FROM photo_maintenance", false},
		{"missing_finish_privilege", "REVOKE EXECUTE ON FUNCTION photo.finish_collection_deletion(uuid,uuid,uuid,uuid) FROM photo_maintenance", false},
		{"public_finish", "GRANT EXECUTE ON FUNCTION photo.finish_collection_deletion(uuid,uuid,uuid,uuid) TO PUBLIC", false},
		{"api_claim", "GRANT EXECUTE ON FUNCTION photo.claim_collection_deletion() TO photo_api", false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			tx, err := db.Begin(ctx)
			if err != nil { t.Fatal(err) }
			defer tx.Rollback(context.WithoutCancel(ctx))
			if scenario.mutation != "" { if _, err = tx.Exec(ctx, scenario.mutation); err != nil { t.Fatal(err) } }
			if _, err = tx.Exec(ctx, "SET LOCAL ROLE photo_maintenance"); err != nil { t.Fatal(err) }
			if err = RequireCollectionCleanup(ctx, tx); (err == nil) != scenario.ready { t.Fatalf("ready=%v error=%v", scenario.ready, err) }
		})
	}
}
