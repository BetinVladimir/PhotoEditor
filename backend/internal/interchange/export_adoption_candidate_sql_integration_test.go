package interchange

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationExportAdoptionRootRequiresCanonicalAuthority(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated DB migration02033 required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, role := range []string{"photo_api", "photo_worker", "photo_scheduler", "photo_gateway", "photo_portal"} {
		var allowed bool
		if err := db.QueryRow(ctx, `SELECT has_function_privilege($1,'photo.v30_export_adoption_root(uuid,uuid,uuid,text,text)','EXECUTE')`, role).Scan(&allowed); err != nil {
			t.Fatal(err)
		}
		if allowed != (role == "photo_api") {
			t.Fatal("unexpected adoption root privilege", role)
		}
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	tid := uuid.New()
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true)`, tid.String()); err != nil {
		t.Fatal(err)
	}
	var root uuid.UUID
	err = tx.QueryRow(ctx, `SELECT artifact_id FROM photo.v30_export_adoption_root($1,$2,$3,$4,$5)`,
		tid, uuid.New(), uuid.New(), "missing-ledger", rawSHA([]byte("request"))).Scan(&root)
	if err == nil {
		t.Fatal("root returned without a canonical result and pending authority")
	}
}
