package interchange

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Requires an isolated database migrated through 02018. The table is only a
// storage primitive until dedicated grant/revoke and issuer functions exist.
func TestImportReviewDecisionStoreSQLIsolation(t *testing.T) {
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
	var rls, forced bool
	if err := db.QueryRow(ctx, `SELECT relrowsecurity,relforcerowsecurity FROM pg_class WHERE oid='photo.v30_import_review_decisions'::regclass`).Scan(&rls, &forced); err != nil || !rls || !forced {
		t.Fatal("review decision store lacks forced tenant isolation", err)
	}
	for _, role := range []string{"photo_api", "photo_scheduler", "photo_worker", "photo_gateway", "photo_portal"} {
		var allowed bool
		if err := db.QueryRow(ctx, `SELECT has_table_privilege($1,'photo.v30_import_review_decisions','SELECT,INSERT,UPDATE,DELETE')`, role).Scan(&allowed); err != nil || allowed {
			t.Fatal("direct review decision DML exposed", role, err)
		}
	}
	var purge string
	if err := db.QueryRow(ctx, `SELECT pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure)`).Scan(&purge); err != nil || !strings.Contains(purge, "'v30_import_review_decisions'") {
		t.Fatal("tenant purge does not include import review decisions", err)
	}
}

// Requires an isolated database migrated through 02019.
func TestImportReviewGrantSQLPrivilegesAndAbsentScope(t *testing.T) {
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
	signature := "photo.grant_v30_import_review(uuid,uuid,uuid,uuid,uuid,text,text,jsonb)"
	for _, role := range []string{"photo_api", "photo_scheduler", "photo_worker", "photo_gateway", "photo_portal"} {
		var allowed bool
		if err := db.QueryRow(ctx, `SELECT has_function_privilege($1,$2,'EXECUTE')`, role, signature).Scan(&allowed); err != nil || allowed != (role == "photo_api") {
			t.Fatal("import review grant privilege drift", role, err)
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
		err := tx.QueryRow(ctx, `SELECT photo.grant_v30_import_review($1,$2,$3,$4,$5,$6,$7,$8::jsonb)`,
			queried, uuid.New(), uuid.New(), uuid.New(), uuid.New(), `{}`, `{}`, `{}`).Scan(&epoch)
		if err != nil || epoch != nil {
			t.Fatal("absent or foreign import received review grant", err)
		}
	}
}

// Requires an isolated database migrated through 02020. A stopped import may
// be revoked, but only its current project editor may inspect or mutate it.
func TestImportReviewRevokeStatusSQLPrivilegesAndAbsentScope(t *testing.T) {
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
		"photo.revoke_v30_import_review(uuid,uuid,uuid,uuid,uuid,jsonb)",
		"photo.v30_import_review_status(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid)",
	} {
		for _, role := range []string{"photo_api", "photo_scheduler", "photo_worker", "photo_gateway", "photo_portal"} {
			var allowed bool
			if err := db.QueryRow(ctx, `SELECT has_function_privilege($1,$2,'EXECUTE')`, role, signature).Scan(&allowed); err != nil || allowed != (role == "photo_api") {
				t.Fatal("import review revoke/status privilege drift", signature, role, err)
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
		if err := tx.QueryRow(ctx, `SELECT photo.revoke_v30_import_review($1,$2,$3,$4,$5,$6::jsonb)`,
			queried, uuid.New(), uuid.New(), uuid.New(), uuid.New(), `{}`).Scan(&epoch); err != nil || epoch != nil {
			t.Fatal("absent or foreign review was revoked", err)
		}
		var status []byte
		if err := tx.QueryRow(ctx, `SELECT photo.v30_import_review_status($1,$2,$3,$4,$5,$6,$7,$8)`,
			queried, uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), "dust", uuid.New()).Scan(&status); err != nil || status != nil {
			t.Fatal("absent or foreign review status exposed", err)
		}
	}
}

// Requires an isolated database migrated through 02021.
func TestImportReviewProposalReadSQLPrivilegesAndAbsentScope(t *testing.T) {
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
	signature := "photo.v30_import_review_proposal_object(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text)"
	for _, role := range []string{"photo_api", "photo_scheduler", "photo_worker", "photo_gateway", "photo_portal"} {
		var allowed bool
		if err := db.QueryRow(ctx, `SELECT has_function_privilege($1,$2,'EXECUTE')`, role, signature).Scan(&allowed); err != nil || allowed != (role == "photo_api") {
			t.Fatal("prepared review proposal read privilege drift", role, err)
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
		rows, err := tx.Query(ctx, `SELECT * FROM photo.v30_import_review_proposal_object($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			queried, uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), "defects", uuid.New(), `{}`)
		if err != nil {
			t.Fatal(err)
		}
		if rows.Next() {
			rows.Close()
			t.Fatal("absent or foreign proposal exposed")
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		rows.Close()
	}
}
