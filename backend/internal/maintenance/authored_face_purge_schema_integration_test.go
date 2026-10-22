package maintenance

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Inspect the installed schema, never migration text. Missing authored review
// tables are a failure on a fully migrated test database, not an optional feature.
func TestAuthoredFaceReviewPurgeSchema(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated fully migrated PostgreSQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var body string
	if err = db.QueryRow(ctx, `SELECT prosrc FROM pg_catalog.pg_proc WHERE oid='photo.finish_tenant_deletion(uuid,uuid)'::pg_catalog.regprocedure`).Scan(&body); err != nil {
		t.Fatal(err)
	}
	array := regexp.MustCompile(`(?s)tables_to_purge constant text\[\]:=ARRAY\[(.*?)\]`).FindStringSubmatch(body)
	if len(array) != 2 {
		t.Fatal("explicit tenant purge allowlist missing")
	}
	position := func(name string) int {
		t.Helper()
		literal := "'" + name + "'"
		if strings.Count(array[1], literal) != 1 {
			t.Fatalf("purge entry must appear exactly once: %s", name)
		}
		return strings.Index(array[1], literal)
	}
	decisions := position("authored_face_selection_decisions")
	candidates := position("authored_face_selection_candidates")
	intents := position("authored_render_intents")
	if decisions >= candidates || candidates >= intents {
		t.Fatal("face review purge must delete decisions, candidates, then render intents")
	}
	for _, table := range []string{"authored_face_selection_decisions", "authored_face_selection_candidates"} {
		t.Run(table, func(t *testing.T) {
			var rls, forced, tenantUUID, ownerDelete, directRuntimeDML bool
			err := db.QueryRow(ctx, `SELECT c.relrowsecurity,c.relforcerowsecurity,
    EXISTS(SELECT FROM pg_catalog.pg_attribute a WHERE a.attrelid=c.oid AND a.attname='tenant_id' AND a.attnum>0 AND NOT a.attisdropped AND a.attnotnull AND a.atttypid='pg_catalog.uuid'::pg_catalog.regtype),
    pg_catalog.has_table_privilege('photo_fn_owner',c.oid,'DELETE'),
    EXISTS(SELECT FROM pg_catalog.pg_roles r WHERE r.rolname IN ('photo_api','photo_worker','photo_scheduler','photo_gateway','photo_portal','photo_auth','photo_mail','photo_publisher') AND (pg_catalog.has_table_privilege(r.oid,c.oid,'INSERT') OR pg_catalog.has_table_privilege(r.oid,c.oid,'UPDATE') OR pg_catalog.has_table_privilege(r.oid,c.oid,'DELETE')))
    FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='photo' AND c.relname=$1 AND c.relkind='r'`, table).Scan(&rls, &forced, &tenantUUID, &ownerDelete, &directRuntimeDML)
			if err != nil {
				t.Fatal("required review table missing or unreadable", err)
			}
			if !rls || !forced || !tenantUUID || !ownerDelete || directRuntimeDML {
				t.Fatalf("unsafe review purge schema: rls=%v forced=%v tenantUUID=%v ownerDelete=%v runtimeDML=%v", rls, forced, tenantUUID, ownerDelete, directRuntimeDML)
			}
			var immutable bool
			if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT FROM pg_catalog.pg_trigger t
                JOIN pg_catalog.pg_proc p ON p.oid=t.tgfoid
                JOIN pg_catalog.pg_namespace n ON n.oid=p.pronamespace
                WHERE t.tgrelid=pg_catalog.to_regclass('photo.'||$1)
                AND NOT t.tgisinternal AND t.tgenabled IN ('O','A') AND t.tgtype=19
                AND n.nspname='photo' AND p.proname='guard_v2_immutable'
                AND t.tgqual IS NULL AND t.tgnargs=0 AND t.tgattr=''::pg_catalog.int2vector)`, table).Scan(&immutable); err != nil || !immutable {
				t.Fatal("enabled unconditional row immutability guard missing", immutable, err)
			}
			parent := "authored_render_intents"
			columns := []string{"tenant_id", "parent_run_id"}
			if table == "authored_face_selection_decisions" {
				parent = "authored_face_selection_candidates"
				columns = []string{"tenant_id", "parent_run_id", "ordinal", "candidate_sha"}
			}
			var scopedFK bool
			if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT FROM pg_catalog.pg_constraint c
                WHERE c.conrelid=pg_catalog.to_regclass('photo.'||$1)
                AND c.confrelid=pg_catalog.to_regclass('photo.'||$2)
                AND c.contype='f' AND c.convalidated AND c.confdeltype='a'
                AND ARRAY(SELECT a.attname::text FROM unnest(c.conkey) WITH ORDINALITY k(num,ord)
                    JOIN pg_catalog.pg_attribute a ON a.attrelid=c.conrelid AND a.attnum=k.num ORDER BY k.ord)=$3::text[]
                AND ARRAY(SELECT a.attname::text FROM unnest(c.confkey) WITH ORDINALITY k(num,ord)
                    JOIN pg_catalog.pg_attribute a ON a.attrelid=c.confrelid AND a.attnum=k.num ORDER BY k.ord)=$3::text[])`, table, parent, columns).Scan(&scopedFK); err != nil || !scopedFK {
				t.Fatal("validated tenant-scoped foreign key missing", scopedFK, err)
			}

		})
	}
}
