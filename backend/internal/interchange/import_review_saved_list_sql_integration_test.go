package interchange

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Requires an isolated database migrated through 02023.
func TestImportReviewSavedListSQLPrivilegesAndAbsentScope(t *testing.T) {
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
	for _, role := range []string{"photo_api", "photo_scheduler", "photo_worker", "photo_gateway", "photo_portal"} {
		var allowed bool
		err := db.QueryRow(ctx, `SELECT has_function_privilege($1,$2,'EXECUTE')`, role,
			"photo.v30_import_review_saved_list(uuid,uuid,uuid,uuid,uuid)").Scan(&allowed)
		if err != nil || allowed != (role == "photo_api") {
			t.Fatal("saved review list privilege drift", role, err)
		}
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	tenantID := uuid.New()
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true)`, tenantID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SET LOCAL ROLE photo_api`); err != nil {
		t.Fatal(err)
	}
	for _, requested := range []uuid.UUID{tenantID, uuid.New()} {
		rows, err := tx.Query(ctx, `SELECT photo.v30_import_review_saved_list($1,$2,$3,$4,$5)`, requested, uuid.New(), uuid.New(), uuid.New(), uuid.New())
		if err != nil {
			t.Fatal(err)
		}
		if rows.Next() {
			rows.Close()
			t.Fatal("absent or foreign saved decision listed")
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		rows.Close()
	}
}
