package maintenance

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The actual role grants must track the cleanup-gated function's closed list.
// audit_log is the sole special case: tenant lifecycle audit rows are retained.
func TestTenantPurgeDeleteGrantsMatchFunctionAllowlist(t *testing.T) {
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
	var definition string
	if err := db.QueryRow(ctx, `SELECT pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure)`).Scan(&definition); err != nil {
		t.Fatal(err)
	}
	array := regexp.MustCompile(`(?s)tables_to_purge constant text\[\]:=ARRAY\[(.*?)\]`).FindStringSubmatch(definition)
	if len(array) != 2 {
		t.Fatal("tenant purge allowlist missing")
	}
	allowed := map[string]bool{"audit_log": true}
	// These rows survive tenant deletion by contract; the V22 lock is deleted
	// by the v2_workflows foreign key, so granting direct DELETE would widen
	// photo_fn_owner beyond the explicit function allowlist.
	retained := map[string]bool{"memberships": true, "deletion_jobs": true}
	cascaded := map[string]string{"v22_analysis_locks": "v2_workflows"}
	for _, match := range regexp.MustCompile(`'([a-z][a-z0-9_]*)'`).FindAllStringSubmatch(array[1], -1) {
		if allowed[match[1]] {
			t.Fatal("duplicate tenant purge entry", match[1])
		}
		allowed[match[1]] = true
	}
	if len(allowed) < 2 {
		t.Fatal("empty tenant purge allowlist")
	}
	rows, err := db.Query(ctx, `SELECT c.relname,
  has_table_privilege('photo_fn_owner',c.oid,'DELETE'),
  EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=c.oid
   AND a.attname='tenant_id' AND a.attnum>0 AND NOT a.attisdropped)
 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname='photo' AND c.relkind IN('r','p')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var table string
		var canDelete, tenantScoped bool
		if err := rows.Scan(&table, &canDelete, &tenantScoped); err != nil {
			t.Fatal(err)
		}
		if allowed[table] != canDelete {
			t.Fatalf("photo_fn_owner DELETE grant drifts from purge allowlist on %s", table)
		}
		if allowed[table] && !tenantScoped {
			t.Fatalf("purge allowlist includes non-tenant table %s", table)
		}
		if (retained[table] || cascaded[table] != "") && !tenantScoped {
			t.Fatalf("tenant lifecycle exception lost tenant_id: %s", table)
		}
		if tenantScoped && !allowed[table] && !retained[table] && cascaded[table] == "" {
			t.Fatalf("tenant-scoped table is neither purged nor retained by contract: %s", table)
		}
		seen[table] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for table := range allowed {
		if !seen[table] {
			t.Fatal("purge allowlist table missing", table)
		}
	}
	for table := range retained {
		if !seen[table] {
			t.Fatal("retained tenant lifecycle table missing", table)
		}
	}
	for table, parent := range cascaded {
		if !seen[table] || !allowed[parent] {
			t.Fatal("cascaded tenant table or purge parent missing", table, parent)
		}
		var cascade bool
		err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_constraint c
  JOIN pg_class child ON child.oid=c.conrelid
  JOIN pg_namespace ns ON ns.oid=child.relnamespace AND ns.nspname='photo'
  JOIN pg_class parent ON parent.oid=c.confrelid
  WHERE child.relname=$1 AND parent.relname=$2 AND c.contype='f'
   AND c.confdeltype='c' AND EXISTS(SELECT 1 FROM pg_attribute a
    WHERE a.attrelid=child.oid AND a.attname='tenant_id'
     AND a.attnum=ANY(c.conkey)))`, table, parent).Scan(&cascade)
		if err != nil || !cascade {
			t.Fatal("tenant purge cascade is missing", table, parent, err)
		}
	}
}

func TestTenantPurgeRemovesHistoricalDeliverySigningPins(t *testing.T) {
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
	var purge, announce string
	if err = db.QueryRow(ctx, `SELECT pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure)`).Scan(&purge); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, `SELECT pg_get_functiondef('photo.announce_v30_delivery_runtime_pinned(uuid,text,uuid[],jsonb)'::regprocedure)`).Scan(&announce); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(purge, "UPDATE photo.v30_delivery_runtime_leases SET") ||
		!strings.Contains(purge, "array_remove(signing_tenants,j.tenant_id)") ||
		!strings.Contains(purge, "jsonb_agg(pin.value ORDER BY pin.ordinal)") ||
		!strings.Contains(announce, "status='active' FOR SHARE") {
		t.Fatal("delivery signing tenant pins can survive metadata purge or be reannounced")
	}
	var schedulerDelete, updateGranted, schedulerPinned, schedulerWindowed bool
	if err = db.QueryRow(ctx, `SELECT
  has_table_privilege('photo_scheduler','photo.v30_delivery_runtime_leases','DELETE'),
  has_table_privilege('photo_fn_owner','photo.v30_delivery_runtime_leases','UPDATE'),
  has_function_privilege('photo_scheduler','photo.announce_v30_delivery_runtime_pinned(uuid,text,uuid[],jsonb)','EXECUTE'),
  has_function_privilege('photo_scheduler','photo.announce_v30_delivery_runtime_windowed(uuid,text,uuid[],jsonb)','EXECUTE')`).
		Scan(&schedulerDelete, &updateGranted, &schedulerPinned, &schedulerWindowed); err != nil {
		t.Fatal(err)
	}
	if schedulerDelete || !updateGranted || schedulerPinned || !schedulerWindowed {
		t.Fatal("delivery runtime purge ACL drift")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, "SET LOCAL ROLE photo_scheduler"); err != nil {
		t.Fatal(err)
	}
	var accepted bool
	err = tx.QueryRow(ctx, `SELECT photo.announce_v30_delivery_runtime_windowed(
  $1,'photo',ARRAY[$2]::uuid[],jsonb_build_array(jsonb_build_object(
   'tenant_id',$2::text,'policy_id',$3::text,'policy_sha',$4::text,
   'not_before',(clock_timestamp()-interval '1 day')::text,
   'not_after',(clock_timestamp()+interval '1 day')::text)))`,
		uuid.New(), uuid.New(), uuid.New(), strings.Repeat("a", 64)).Scan(&accepted)
	if err != nil || accepted {
		t.Fatal("unknown tenant signing runtime accepted", err, accepted)
	}
}

func TestGlobalRuntimeTablesDoNotGiveFunctionOwnerImplicitDelete(t *testing.T) {
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
	var login, inherit, bypass, super, member bool
	if err = db.QueryRow(ctx, `SELECT role.rolcanlogin,role.rolinherit,
  role.rolbypassrls,role.rolsuper,
  pg_has_role('photo_fn_owner','photo_runtime_owner','member')
  FROM pg_roles role WHERE role.rolname='photo_runtime_owner'`).
		Scan(&login, &inherit, &bypass, &super, &member); err != nil {
		t.Fatal(err)
	}
	if login || inherit || bypass || super || member {
		t.Fatal("global runtime table owner is not isolated")
	}
	for _, table := range []struct {
		name  string
		lease bool
	}{
		{"v30_import_runtime_leases", true},
		{"v30_import_adopter_runtime_leases", true},
		{"v30_export_runtime_leases", true},
		{"v30_delivery_runtime_leases", true},
		{"v31_signing_adopter_runtime_leases", true},
		{"capacity_policy_timings", false},
		{"restore_quarantine_runs", false},
		{"restore_quarantine_versions", false},
	} {
		var owner string
		var read, insert, update, deleteGranted bool
		err = db.QueryRow(ctx, `SELECT pg_get_userbyid(c.relowner),
  has_table_privilege('photo_fn_owner',c.oid,'SELECT'),
  has_table_privilege('photo_fn_owner',c.oid,'INSERT'),
  has_table_privilege('photo_fn_owner',c.oid,'UPDATE'),
  has_table_privilege('photo_fn_owner',c.oid,'DELETE')
  FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
  WHERE n.nspname='photo' AND c.relname=$1`, table.name).
			Scan(&owner, &read, &insert, &update, &deleteGranted)
		if err != nil || owner != "photo_runtime_owner" || !read ||
			insert != table.lease || update != table.lease || deleteGranted {
			t.Fatal("global runtime table ACL drift", table.name, owner, err)
		}
	}
}

// Newer editor/culling/export tables must use the same checked purge array as
// older tenant metadata, rather than an invisible second list of inline DELETEs.
func TestTenantPurgeNormalizedEditorAndExportAllowlist(t *testing.T) {
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL")
 if dsn=="" { t.Skip("isolated fully migrated PostgreSQL required") }
 ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil { t.Fatal(err) };defer db.Close()
 var definition string
 if err=db.QueryRow(ctx,`SELECT pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure)`).Scan(&definition);err!=nil { t.Fatal(err) }
 array:=regexp.MustCompile(`(?s)tables_to_purge constant text\[\]:=ARRAY\[(.*?)\]`).FindStringSubmatch(definition)
 if len(array)!=2 { t.Fatal("purge allowlist missing") }
 for _,name:=range []string{"export_delivery_tickets","export_delivery_batches","editor_effect_builds","editor_recipe_bridges","apply_culling_reviews"} {
  if strings.Count(array[1],"'"+name+"'")!=1 || strings.Contains(definition,"DELETE FROM photo."+name+" WHERE") { t.Fatal("purge list not normalized",name) }
 }
}

func TestEditorGCStartupRejectsDisabledReferenceGuard(t *testing.T) {
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL")
 if dsn=="" { t.Skip("isolated fully migrated PostgreSQL required") }
 ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil { t.Fatal(err) };defer db.Close()
 tx,err:=db.Begin(ctx);if err!=nil { t.Fatal(err) };defer tx.Rollback(context.Background())
 if err=RequireEditorGCSchema(ctx,tx);err!=nil { t.Fatal(err) }
 if _,err=tx.Exec(ctx,"ALTER TABLE photo.editor_effect_builds DISABLE TRIGGER ready_artifact_reference");err!=nil { t.Fatal(err) }
 if err=RequireEditorGCSchema(ctx,tx);err==nil { t.Fatal("cleanup accepted disabled editor artifact guard") }
}
