package interchange

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationExportAdoptionClaimPrivateRoleAndMissingLease(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL through migration02035 required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, signature := range []string{
		"photo.v30_claim_next_export_adoption()",
		"photo.v30_export_adoption_claim_current(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid)",
	} {
		for _, role := range []string{"photo_scheduler", "photo_api", "photo_worker", "photo_gateway", "photo_portal"} {
			var allowed bool
			if err := db.QueryRow(ctx, `SELECT has_function_privilege($1,$2,'EXECUTE')`, role, signature).Scan(&allowed); err != nil {
				t.Fatal(err)
			}
			if allowed != (role == "photo_scheduler") {
				t.Fatal("unexpected export claim privilege", signature, role)
			}
		}
	}
	var current bool
	err = db.QueryRow(ctx, `SELECT photo.v30_export_adoption_claim_current($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), "missing-claim",
		rawSHA([]byte("request")), uuid.New()).Scan(&current)
	if err != nil || current {
		t.Fatal("missing export lease accepted", err)
	}
}
