package v2draft

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Requires the isolated migration-complete test database; never provisions or
// mutates learning/consent data to manufacture an authorization result.
func TestIntegrationRenderConsentDiagnosticUnknownScopeAndACL(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" { t.Skip("isolated PostgreSQL required") }
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil { t.Fatal(err) }
	defer pool.Close()
	apiConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil { t.Fatal(err) }
	apiConfig.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, err := pgxpool.NewWithConfig(ctx, apiConfig)
	if err != nil { t.Fatal(err) }
	defer api.Close()
	if err := RequireRenderConsentDiagnostic(ctx, api); err != nil { t.Fatal(err) }
	const signature = "photo.v26_render_model_consent_revoked(uuid,uuid,uuid,uuid,text,uuid,uuid,text,uuid,uuid)"
	for _, role := range []string{"photo_api", "photo_worker", "photo_scheduler"} {
		var allowed bool
		if err := pool.QueryRow(ctx, "SELECT has_function_privilege($1,$2,'EXECUTE')", role, signature).Scan(&allowed); err != nil { t.Fatal(err) }
		if allowed != (role == "photo_api") { t.Fatalf("unexpected diagnostic ACL for %s: %v", role, allowed) }
	}
	tx, err := pool.Begin(ctx)
	if err != nil { t.Fatal(err) }
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_api"); err != nil { t.Fatal(err) }
	tenant := uuid.New()
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil { t.Fatal(err) }
	for _, scope := range []uuid.UUID{tenant, uuid.New()} {
		var revoked bool
		if err := tx.QueryRow(ctx, `SELECT photo.v26_render_model_consent_revoked($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
			scope, uuid.New(), uuid.New(), uuid.New(), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			uuid.New(), uuid.New(), "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			uuid.New(), uuid.New()).Scan(&revoked); err != nil { t.Fatal(err) }
		if revoked { t.Fatal("unknown or foreign scope exposed consent diagnosis") }
	}
}
