package interchange

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationSchedulerExportEvidencePrivateAndUnclaimed(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL through migration02043 required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, signature := range []string{
		"photo.save_v30_export_adoption_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,text,text)",
		"photo.adopt_v30_export_adoption(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,text,text)",
	} {
		for _, role := range []string{"photo_scheduler", "photo_api", "photo_worker", "photo_gateway", "photo_portal"} {
			var allowed bool
			if err := db.QueryRow(ctx, `SELECT has_function_privilege($1,$2,'EXECUTE')`,
				role, signature).Scan(&allowed); err != nil || allowed != (role == "photo_scheduler") {
				t.Fatalf("unexpected evidence grant for %s %s: %v", signature, role, err)
			}
		}
	}
	var saved bool
	err = db.QueryRow(ctx, `SELECT photo.save_v30_export_adoption_evidence(
 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(),
		"missing-claim", rawSHA([]byte("request")), uuid.New(), "{}", rawSHA([]byte("{}"))).Scan(&saved)
	if err != nil || saved {
		t.Fatalf("unclaimed evidence accepted: %v", err)
	}
	err = db.QueryRow(ctx, `SELECT photo.adopt_v30_export_adoption(
 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(),
		"missing-claim", rawSHA([]byte("request")), uuid.New(), "{}", rawSHA([]byte("{}"))).Scan(&saved)
	if err != nil || saved {
		t.Fatalf("unclaimed adoption accepted: %v", err)
	}
}
