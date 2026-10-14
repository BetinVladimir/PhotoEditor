package interchange

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Requires an isolated database migrated through 02003.
func TestImportMaskConfirmationSQLPrivilegesAndAbsentScope(t *testing.T) {
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
	for _, signature := range []string{
		"photo.v30_import_person_mask_confirmation_live(uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,uuid,text)",
		"photo.v30_import_person_mask_prepared_live(uuid,uuid,uuid,uuid,text,text)",
	} {
		for _, role := range []string{"photo_scheduler", "photo_api", "photo_worker", "photo_gateway", "photo_portal"} {
			var allowed bool
			if err := db.QueryRow(ctx, `SELECT has_function_privilege($1,$2,'EXECUTE')`, role, signature).Scan(&allowed); err != nil {
				t.Fatal(err)
			}
			if allowed != (role == "photo_scheduler") {
				t.Fatalf("confirmation read privilege drift: %s %s", signature, role)
			}
		}
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	tenant := uuid.New()
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true)`, tenant.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SET LOCAL ROLE photo_scheduler`); err != nil {
		t.Fatal(err)
	}
	for _, queried := range []uuid.UUID{tenant, uuid.New()} {
		var epoch *int64
		err := tx.QueryRow(ctx, `SELECT photo.v30_import_person_mask_prepared_live($1,$2,$3,$4,$5,$6)`,
			queried, uuid.New(), uuid.New(), uuid.New(), rawSHA([]byte("mask")), `{}`).Scan(&epoch)
		if err != nil || epoch != nil {
			t.Fatal("absent or foreign confirmation was accepted", err)
		}
	}
}

func TestImportMaskDecisionSQLPrivilegesAndAbsentScope(t *testing.T) {
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
	for _, signature := range []string{
		"photo.grant_v30_import_person_mask(uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,text,uuid)",
		"photo.revoke_v30_import_person_mask(uuid,uuid,uuid,uuid,uuid,uuid,uuid,uuid)",
	} {
		for _, role := range []string{"photo_api", "photo_scheduler", "photo_worker", "photo_gateway", "photo_portal"} {
			var allowed bool
			if err := db.QueryRow(ctx, `SELECT has_function_privilege($1,$2,'EXECUTE')`, role, signature).Scan(&allowed); err != nil {
				t.Fatal(err)
			}
			if allowed != (role == "photo_api") {
				t.Fatalf("confirmation decision privilege drift: %s %s", signature, role)
			}
		}
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	tenant := uuid.New()
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true)`, tenant.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SET LOCAL ROLE photo_api`); err != nil {
		t.Fatal(err)
	}
	for _, queried := range []uuid.UUID{tenant, uuid.New()} {
		var epoch *int64
		err := tx.QueryRow(ctx, `SELECT photo.grant_v30_import_person_mask($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
			queried, uuid.New(), uuid.New(), uuid.New(), uuid.New(), `{}`, uuid.New(), `{}`, uuid.New(), `{}`, `{}`, uuid.New()).Scan(&epoch)
		if err != nil || epoch != nil {
			t.Fatal("absent or foreign import received confirmation", err)
		}
		err = tx.QueryRow(ctx, `SELECT photo.revoke_v30_import_person_mask($1,$2,$3,$4,$5,$6,$7,$8)`,
			queried, uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()).Scan(&epoch)
		if err != nil || epoch != nil {
			t.Fatal("absent or foreign import received revocation receipt", err)
		}
	}
}

// Requires an isolated database migrated through 02006. The API may inspect
// saved decisions, but scheduler and workers cannot enumerate user grants.
func TestImportMaskConfirmationStatusSQLPrivilegesAndAbsentScope(t *testing.T) {
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
	signature := "photo.v30_import_person_mask_confirmation_status(uuid,uuid,uuid,uuid,uuid)"
	for _, role := range []string{"photo_api", "photo_scheduler", "photo_worker", "photo_gateway", "photo_portal"} {
		var allowed bool
		if err := db.QueryRow(ctx, `SELECT has_function_privilege($1,$2,'EXECUTE')`, role, signature).Scan(&allowed); err != nil {
			t.Fatal(err)
		}
		if allowed != (role == "photo_api") {
			t.Fatalf("confirmation status privilege drift: %s", role)
		}
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	tenant := uuid.New()
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true)`, tenant.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SET LOCAL ROLE photo_api`); err != nil {
		t.Fatal(err)
	}
	for _, queried := range []uuid.UUID{tenant, uuid.New()} {
		var count int
		err := tx.QueryRow(ctx, `SELECT count(*) FROM photo.v30_import_person_mask_confirmation_status($1,$2,$3,$4,$5)`,
			queried, uuid.New(), uuid.New(), uuid.New(), uuid.New()).Scan(&count)
		if err != nil || count != 0 {
			t.Fatal("absent or foreign status scope disclosed decisions", err)
		}
	}
}

// Requires an isolated database migrated through 02007.
func TestImportMaskWorkerLiveSQLPrivilegesAndAbsentLease(t *testing.T) {
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
	signature := "photo.v30_import_worker_person_mask_live(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,uuid,uuid,uuid,uuid,text)"
	for _, role := range []string{"photo_worker", "photo_api", "photo_scheduler", "photo_gateway", "photo_portal"} {
		var allowed bool
		if err := db.QueryRow(ctx, `SELECT has_function_privilege($1,$2,'EXECUTE')`, role, signature).Scan(&allowed); err != nil {
			t.Fatal(err)
		}
		if allowed != (role == "photo_worker") {
			t.Fatalf("worker mask grant privilege drift: %s", role)
		}
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SET LOCAL ROLE photo_worker`); err != nil {
		t.Fatal(err)
	}
	var epoch *int64
	err = tx.QueryRow(ctx, `SELECT photo.v30_import_worker_person_mask_live($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		uuid.New(), uuid.New(), uuid.New(), 1, int64(1), uuid.New(), uuid.New(), rawSHA([]byte("params")), "{}", rawSHA([]byte("digest")),
		uuid.New(), uuid.New(), uuid.New(), uuid.New(), rawSHA([]byte("mask"))).Scan(&epoch)
	if err != nil || epoch != nil {
		t.Fatal("absent worker lease received person-mask grant", err)
	}
}
