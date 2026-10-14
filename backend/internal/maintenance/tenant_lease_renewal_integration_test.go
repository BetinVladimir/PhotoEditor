package maintenance

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationTenantDeletionLeaseRenewalAuthority(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated DB migrations through 01886 required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	const signature = "photo.renew_tenant_deletion_lease(uuid,uuid,uuid)"
	var owner, definition string
	var secdef bool
	if err := db.QueryRow(ctx, `SELECT pg_get_userbyid(proowner),prosecdef,pg_get_functiondef(oid) FROM pg_proc WHERE oid=$1::regprocedure`, signature).Scan(&owner, &secdef, &definition); err != nil {
		t.Fatal(err)
	}
	if owner != "photo_fn_owner" || !secdef {
		t.Fatal("renewal authority")
	}
	for _, required := range []string{"cleanup_token=p_token", "cleanup_lease_until>clock_timestamp()", "status='deleting'", "resource_id=p_tenant"} {
		if !strings.Contains(definition, required) {
			t.Fatal("renewal fence absent", required)
		}
	}
	for _, role := range []string{"photo_api", "photo_worker", "photo_scheduler", "photo_gateway", "photo_portal", "photo_maintenance"} {
		var allowed bool
		if err := db.QueryRow(ctx, `SELECT has_function_privilege($1,$2,'EXECUTE')`, role, signature).Scan(&allowed); err != nil {
			t.Fatal(err)
		}
		if allowed != (role == "photo_maintenance") {
			t.Fatal("renewal role", role, allowed)
		}
	}
	var renewed bool
	if err := db.QueryRow(ctx, `SELECT photo.renew_tenant_deletion_lease($1,$2,$3)`, uuid.New(), uuid.New(), uuid.New()).Scan(&renewed); err != nil || renewed {
		t.Fatal("unleased renewal accepted", renewed, err)
	}
}
