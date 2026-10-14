package interchange

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationSchedulerWorkingArrayRequiresExactClaim(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL through migration02045 required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	signature := "photo.v30_export_adoption_working_array(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,text)"
	for _, role := range []string{"photo_scheduler", "photo_api", "photo_worker", "photo_gateway", "photo_portal"} {
		var allowed bool
		if err := db.QueryRow(ctx, `SELECT has_function_privilege($1,$2,'EXECUTE')`,
			role, signature).Scan(&allowed); err != nil || allowed != (role == "photo_scheduler") {
			t.Fatalf("unexpected working-array privilege %s: %v", role, err)
		}
	}
	var ready bool
	err = db.QueryRow(ctx, `SELECT photo.v30_export_adoption_working_array(
 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(),
		"missing-claim", rawSHA([]byte("request")), uuid.New(), uuid.New(),
		rawSHA([]byte("parent")), uuid.New(), rawSHA([]byte("array")), "WorkingPixels").Scan(&ready)
	if err != nil || ready {
		t.Fatalf("unclaimed working array accepted: %v", err)
	}
}
