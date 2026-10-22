package run

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
)

// Catalog-only cases require no accepted Editor origin or rendering SDK.
func TestIntegrationEditorIdentityStartupCatalogDrift(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated DB required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	owner, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer owner.Close()
	const sig = "(uuid,uuid,uuid,uuid,text,uuid,text,uuid,uuid,bigint,uuid,uuid)"
	for _, name := range []string{"valid", "wrong_role", "missing_helper", "helper_source", "origin_source", "helper_path", "helper_security", "helper_volatility", "helper_parallel", "helper_public_acl", "helper_grantable_api", "helper_worker_acl", "helper_no_api", "users_table_select", "users_column_select", "users_column_update", "sessions_table_update", "sessions_column_select", "sessions_column_update", "portal_users_column_select", "owner_users_select", "owner_users_update", "owner_sessions_select", "owner_sessions_update"} {
		t.Run(name, func(t *testing.T) {
			tx, e := owner.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer tx.Rollback(context.Background())
			exec := func(sql string) {
				t.Helper()
				if _, e := tx.Exec(ctx, sql); e != nil {
					t.Fatal("catalog mutation", e)
				}
			}
			helper := "photo.lock_editor_effect_identity" + sig
			switch name {
			case "missing_helper":
				exec("DROP FUNCTION " + helper)
			case "helper_source", "origin_source":
				function := helper
				if name == "origin_source" {
					function = "photo.editor_effect_origin_scope" + sig
				}
				var body, definition string
				if e = tx.QueryRow(ctx, "SELECT p.prosrc,pg_get_functiondef(p.oid) FROM pg_proc p WHERE oid=to_regprocedure($1)", function).Scan(&body, &definition); e != nil {
					t.Fatal(e)
				}
				exec(strings.Replace(definition, body, body+"\n-- deliberate drift\n", 1))
			case "helper_path":
				exec("ALTER FUNCTION " + helper + " SET search_path=pg_catalog")
			case "helper_security":
				exec("ALTER FUNCTION " + helper + " SECURITY INVOKER")
			case "helper_volatility":
				exec("ALTER FUNCTION " + helper + " STABLE")
			case "helper_parallel":
				exec("ALTER FUNCTION " + helper + " PARALLEL SAFE")
			case "helper_public_acl":
				exec("GRANT EXECUTE ON FUNCTION " + helper + " TO PUBLIC")
			case "helper_grantable_api":
				exec("GRANT EXECUTE ON FUNCTION " + helper + " TO photo_api WITH GRANT OPTION")
			case "helper_worker_acl":
				exec("GRANT EXECUTE ON FUNCTION " + helper + " TO photo_worker")
			case "helper_no_api":
				exec("REVOKE EXECUTE ON FUNCTION " + helper + " FROM photo_api")
			case "users_table_select":
				exec("GRANT SELECT ON photo.users TO photo_api")
			case "users_column_select":
				exec("GRANT SELECT(id) ON photo.users TO photo_api")
			case "users_column_update":
				exec("GRANT UPDATE(id) ON photo.users TO photo_api")
			case "sessions_table_update":
				exec("GRANT UPDATE ON photo.sessions TO photo_api")
			case "sessions_column_select":
				exec("GRANT SELECT(id) ON photo.sessions TO photo_api")
			case "sessions_column_update":
				exec("GRANT UPDATE(id) ON photo.sessions TO photo_api")
			case "portal_users_column_select":
				exec("GRANT SELECT(id) ON photo.users TO photo_portal")
			case "owner_users_select":
				exec("REVOKE SELECT ON photo.users FROM photo_fn_owner")
			case "owner_users_update":
				exec("REVOKE UPDATE ON photo.users FROM photo_fn_owner")
			case "owner_sessions_select":
				exec("REVOKE SELECT ON photo.sessions FROM photo_fn_owner")
			case "owner_sessions_update":
				exec("REVOKE UPDATE ON photo.sessions FROM photo_fn_owner")
			}
			if name != "wrong_role" {
				exec("SET LOCAL ROLE photo_api")
			}
			e = RequireEditorIdentityInstallation(ctx, tx)
			if (e == nil) != (name == "valid") {
				t.Fatal("startup catalog authority", name, e)
			}
		})
	}
}
