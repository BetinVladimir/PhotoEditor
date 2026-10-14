package interchange

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Requires an isolated database migrated through 02024.
func TestImportReviewLiveSQLPrivilegesAndAbsentScope(t *testing.T) {
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
	signature := "photo.v30_import_review_live_decision(uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,text)"
	for _, role := range []string{"photo_scheduler", "photo_api", "photo_worker", "photo_gateway", "photo_portal"} {
		var allowed bool
		if err := db.QueryRow(ctx, `SELECT has_function_privilege($1,$2,'EXECUTE')`, role, signature).Scan(&allowed); err != nil || allowed != (role == "photo_scheduler") {
			t.Fatal("live review reader privilege drift", role, err)
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
	if _, err := tx.Exec(ctx, `SET LOCAL ROLE photo_scheduler`); err != nil {
		t.Fatal(err)
	}
	for _, requested := range []uuid.UUID{tenantID, uuid.New()} {
		rows, err := tx.Query(ctx, `SELECT decision FROM photo.v30_import_review_live_decision($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
			requested, uuid.New(), uuid.New(), uuid.New(), "dust", uuid.New(), rawSHA([]byte("proposal")), uuid.New(), rawSHA([]byte("base")), nil, nil, `{}`)
		if err != nil {
			t.Fatal(err)
		}
		if rows.Next() {
			rows.Close()
			t.Fatal("absent or foreign live decision read")
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		rows.Close()
	}
}
