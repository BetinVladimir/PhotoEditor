package workflow

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
)

// Requires a separately migrated isolated DB; this test never applies migrations.
func TestMultiPreviewQuotaSQLPrivilegesAndTenantRefusal(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated DB required")
	}
	ctx := context.Background()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	signatures := []string{"photo.reserve_v29_multi_preview(uuid,uuid)", "photo.release_v29_multi_preview_quota(uuid,uuid)", "photo.seal_v29_multi_preview(uuid,uuid,text,text,jsonb,jsonb)"}
	for _, signature := range signatures {
		for _, role := range []string{"photo_scheduler", "photo_api", "photo_worker", "photo_portal", "photo_gateway"} {
			var allowed bool
			if err := pool.QueryRow(ctx, "SELECT has_function_privilege($1,$2,'EXECUTE')", role, signature).Scan(&allowed); err != nil {
				t.Fatal(err)
			}
			if allowed != (role == "photo_scheduler") {
				t.Fatalf("unexpected quota permission %s %s %v", role, signature, allowed)
			}
		}
	}
	var privateAllowed bool
	if err := pool.QueryRow(ctx, "SELECT has_function_privilege('photo_scheduler','photo.v29_multi_preview_intent_source(uuid,uuid)','EXECUTE')").Scan(&privateAllowed); err != nil {
		t.Fatal(err)
	}
	if privateAllowed {
		t.Fatal("scheduler can bypass private saved-intent resolver")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	tenant, other, run := uuid.New(), uuid.New(), uuid.New()
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		t.Fatal(err)
	}
	var sealed bool
	if err := tx.QueryRow(ctx, "SELECT photo.seal_v29_multi_preview($1,$2,'{}','{}','{}'::jsonb,'[]'::jsonb)", other, run).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if sealed {
		t.Fatal("foreign tenant activated preview")
	}

	for _, function := range []string{"reserve_v29_multi_preview", "release_v29_multi_preview_quota"} {
		var accepted bool
		// Function name is a literal allowlist defined immediately above.
		if err := tx.QueryRow(ctx, "SELECT photo."+function+"($1,$2)", other, run).Scan(&accepted); err != nil {
			t.Fatal(err)
		}
		if accepted {
			t.Fatalf("foreign tenant accepted by %s", function)
		}
	}
}
