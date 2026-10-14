package interchange

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationSchedulerStyleBackgroundRequiresClaim(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL through migration02090 required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	signature := "photo.v30_export_adoption_style_background(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text,text,uuid,text,bigint,text,text)"
	for _, role := range []string{"photo_scheduler", "photo_api", "photo_worker", "photo_gateway", "photo_portal"} {
		var allowed bool
		if err := db.QueryRow(ctx, `SELECT has_function_privilege($1,$2,'EXECUTE')`,
			role, signature).Scan(&allowed); err != nil || allowed != (role == "photo_scheduler") {
			t.Fatalf("unexpected background privilege %s: %v", role, err)
		}
	}
	var count int
	err = db.QueryRow(ctx, `SELECT count(*) FROM photo.v30_export_adoption_style_background(
 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`,
		uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(),
		uuid.New(), "missing-claim", rawSHA([]byte("request")), uuid.New(),
		uuid.New(), uuid.New(), rawSHA([]byte("style")), "studio",
		uuid.New(), rawSHA([]byte("source")), int64(100), "image/png", "photos").Scan(&count)
	if err != nil || count != 0 {
		t.Fatalf("unclaimed background accepted: %v", err)
	}
}
