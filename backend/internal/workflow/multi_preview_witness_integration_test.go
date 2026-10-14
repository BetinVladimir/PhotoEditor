package workflow

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
)

// Uses a separately migrated isolated database; never applies migrations.
// This proves privilege isolation only, not successful witness/closure admission.
func TestMultiPreviewWitnessGateIsPrivate(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated DB required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, signature := range []string{"photo.v29_multi_preview_witnesses_current(uuid,uuid,text,text,jsonb)", "photo.v29_multi_preview_originals_current(uuid,uuid,jsonb)"} {
		for _, role := range []string{"photo_scheduler", "photo_api", "photo_worker", "photo_portal", "photo_gateway", "photo_fn_owner"} {
			var allowed bool
			if err := pool.QueryRow(ctx, "SELECT has_function_privilege($1,$2,'EXECUTE')", role, signature).Scan(&allowed); err != nil {
				t.Fatal(err)
			}
			if allowed != (role == "photo_fn_owner") {
				t.Fatalf("unexpected witness gate privilege: %s %v", role, allowed)
			}
		}
	}
}
