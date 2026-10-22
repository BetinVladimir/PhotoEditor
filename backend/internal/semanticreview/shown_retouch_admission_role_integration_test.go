package semanticreview

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Transactional ACL changes are restricted to the disposable test database.
func TestIntegrationShownRetouchStartupACL(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" || os.Getenv("PHOTO_TEST_DISPOSABLE") != "1" {
		t.Skip("explicit disposable migrated database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	connection, err := pgx.Connect(ctx, dsn)
	if err != nil { t.Fatal("disposable database unavailable") }
	defer connection.Close(context.Background())
	tx, err := connection.Begin(ctx)
	if err != nil { t.Fatal(err) }
	defer tx.Rollback(context.Background())
	execute := func(statement string) {
		t.Helper()
		if _, err := tx.Exec(ctx, statement); err != nil { t.Fatal(err) }
	}
	execute("SET LOCAL ROLE photo_api")
	if err := verifyShownRetouchAdmissionRole(ctx, tx); !errors.Is(err, ErrUnavailable) {
		t.Fatal("API role accepted for private retouch commit", err)
	}
	execute("RESET ROLE")
	execute("SET LOCAL ROLE photo_semantic_admission")
	if err := verifyShownRetouchAdmissionRole(ctx, tx); err != nil { t.Fatal("installed retouch ACL refused", err) }
	execute("RESET ROLE")
	execute("REVOKE EXECUTE ON FUNCTION photo.commit_semantic_shown_retouch_apply_review(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,jsonb,uuid,jsonb) FROM photo_semantic_admission")
	execute("SET LOCAL ROLE photo_semantic_admission")
	if err := verifyShownRetouchAdmissionRole(ctx, tx); !errors.Is(err, ErrUnavailable) {
		t.Fatal("revoked exact retouch authority accepted", err)
	}
	execute("RESET ROLE")
	execute("GRANT EXECUTE ON FUNCTION photo.commit_semantic_shown_retouch_apply_review(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,jsonb,uuid,jsonb) TO photo_semantic_admission")
	execute("SET LOCAL ROLE photo_semantic_admission")
	if err := verifyShownRetouchAdmissionRole(ctx, tx); err != nil { t.Fatal("restored own ACL refused", err) }
	execute("RESET ROLE")
	// Configuration corruption is transactional; the production body is never
	// replaced or invoked while its definer configuration is invalid.
	execute("ALTER FUNCTION photo.commit_semantic_shown_retouch_apply_review(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,jsonb,uuid,jsonb) SET search_path=public")
	execute("SET LOCAL ROLE photo_semantic_admission")
	if err := verifyShownRetouchAdmissionRole(ctx, tx); !errors.Is(err, ErrUnavailable) {
		t.Fatal("unsafe definer search path accepted", err)
	}
	execute("RESET ROLE")
	execute("ALTER FUNCTION photo.commit_semantic_shown_retouch_apply_review(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,jsonb,uuid,jsonb) SET search_path=pg_catalog,photo")
	execute("ALTER FUNCTION photo.commit_semantic_shown_retouch_apply_review(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,jsonb,uuid,jsonb) SECURITY INVOKER")
	execute("SET LOCAL ROLE photo_semantic_admission")
	if err := verifyShownRetouchAdmissionRole(ctx, tx); !errors.Is(err, ErrUnavailable) {
		t.Fatal("lost definer authority accepted", err)
	}
	execute("RESET ROLE")
	execute("ALTER FUNCTION photo.commit_semantic_shown_retouch_apply_review(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,jsonb,uuid,jsonb) SECURITY DEFINER")
	execute("SET LOCAL ROLE photo_semantic_admission")
	if err := verifyShownRetouchAdmissionRole(ctx, tx); err != nil { t.Fatal("restored pinned definer configuration refused", err) }
	execute("RESET ROLE")
	for _, name := range []string{"lock_semantic_shown_retouch_decision_batch", "semantic_shown_retouch_batch_nodes_bound",
		"create_semantic_shown_retouch_apply_draft_shell", "reserve_semantic_shown_retouch_apply_quota",
		"create_semantic_shown_retouch_apply_reserved_draft", "register_semantic_shown_retouch_apply_inputs", "seal_semantic_shown_retouch_apply_inputs",
		"activate_semantic_shown_retouch_apply_child", "finish_semantic_shown_retouch_apply_review"} {
		var signature string
		if err := tx.QueryRow(ctx, "SELECT p.oid::regprocedure::text FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='photo' AND p.proname=$1", name).Scan(&signature); err != nil { t.Fatal(err) }
		// Signatures originate only from the catalog for the fixed name allowlist.
		execute("GRANT EXECUTE ON FUNCTION "+signature+" TO photo_semantic_admission")
		execute("SET LOCAL ROLE photo_semantic_admission")
		if err := verifyShownRetouchAdmissionRole(ctx, tx); !errors.Is(err, ErrUnavailable) { t.Fatal("private transaction component exposed",name,err) }
		execute("RESET ROLE")
		execute("REVOKE EXECUTE ON FUNCTION "+signature+" FROM photo_semantic_admission")
		execute("SET LOCAL ROLE photo_semantic_admission")
		if err := verifyShownRetouchAdmissionRole(ctx, tx); err != nil { t.Fatal("restored private component refused",name,err) }
		execute("RESET ROLE")
	}
	for _, role := range []string{"photo_api", "photo_worker", "photo_scheduler", "photo_portal", "photo_gateway"} {
		const signature = "photo.commit_semantic_shown_retouch_apply_review(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,jsonb,uuid,jsonb)"
		execute("GRANT EXECUTE ON FUNCTION "+signature+" TO "+role)
		execute("SET LOCAL ROLE photo_semantic_admission")
		if err := verifyShownRetouchAdmissionRole(ctx, tx); !errors.Is(err, ErrUnavailable) { t.Fatal("retouch commit leaked to runtime role",role,err) }
		execute("RESET ROLE")
		execute("REVOKE EXECUTE ON FUNCTION "+signature+" FROM "+role)
	}
	// Count every overload, including invoker/unsafe/publicly exposed ones.
	// Filtering to only valid private signatures would hide an extra function.
	for _, name := range []string{"commit_semantic_shown_retouch_apply_review",
		"lock_semantic_shown_retouch_decision_batch", "semantic_shown_retouch_batch_nodes_bound",
		"create_semantic_shown_retouch_apply_draft_shell", "reserve_semantic_shown_retouch_apply_quota",
		"create_semantic_shown_retouch_apply_reserved_draft", "register_semantic_shown_retouch_apply_inputs", "seal_semantic_shown_retouch_apply_inputs",
		"activate_semantic_shown_retouch_apply_child", "finish_semantic_shown_retouch_apply_review"} {
		signature := "photo." + name + "(text)"
		var absent bool
		if err := tx.QueryRow(ctx, "SELECT to_regprocedure($1) IS NULL", signature).Scan(&absent); err != nil || !absent {
			t.Fatal("unexpected preexisting test overload", name, err)
		}
		execute("CREATE FUNCTION photo." + name + "(text) RETURNS boolean LANGUAGE sql SECURITY INVOKER AS $$SELECT false$$")
		execute("SET LOCAL ROLE photo_semantic_admission")
		if err := verifyShownRetouchAdmissionRole(ctx, tx); !errors.Is(err, ErrUnavailable) { t.Fatal("extra invoker overload accepted", name, err) }
		execute("RESET ROLE")
		execute("ALTER FUNCTION " + signature + " OWNER TO photo_fn_owner")
		execute("ALTER FUNCTION " + signature + " SECURITY DEFINER")
		execute("ALTER FUNCTION " + signature + " SET search_path=pg_catalog,photo")
		execute("REVOKE ALL ON FUNCTION " + signature + " FROM PUBLIC,photo_semantic_admission")
		execute("SET LOCAL ROLE photo_semantic_admission")
		if err := verifyShownRetouchAdmissionRole(ctx, tx); !errors.Is(err, ErrUnavailable) { t.Fatal("extra private overload accepted", name, err) }
		execute("RESET ROLE")
		execute("DROP FUNCTION " + signature)
		execute("SET LOCAL ROLE photo_semantic_admission")
		if err := verifyShownRetouchAdmissionRole(ctx, tx); err != nil { t.Fatal("overload cleanup did not restore ACL", name, err) }
		execute("RESET ROLE")
	}
	execute("SET LOCAL ROLE photo_semantic_admission")
	if err := verifyShownRetouchAdmissionRole(ctx, tx); err != nil { t.Fatal("final restored ACL refused",err) }
	t.Log("V27_RETOUCH_STARTUP_ACL_OK")
}
