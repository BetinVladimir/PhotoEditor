package interchange

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationPersonOriginReceiptStorageIsNotUserGrant(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated DB migration02028 required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	const signature = "photo.v30_import_person_origin_receipt_live(uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,text)"
	for _, role := range []string{"photo_api", "photo_worker", "photo_scheduler", "photo_gateway", "photo_portal"} {
		var canRead, canWrite, canCall bool
		if err := db.QueryRow(ctx, `SELECT has_table_privilege($1,'photo.v30_import_person_origin_receipts','SELECT'),
 has_table_privilege($1,'photo.v30_import_person_origin_receipts','INSERT'),has_function_privilege($1,$2,'EXECUTE')`, role, signature).Scan(&canRead, &canWrite, &canCall); err != nil {
			t.Fatal(err)
		}
		if canRead || canWrite || canCall != (role == "photo_scheduler") {
			t.Fatal("receipt authority leaked to role", role)
		}
	}
	var live bool
	if err := db.QueryRow(ctx, `SELECT photo.v30_import_person_origin_receipt_live(
 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), rawSHA([]byte("recipe")),
		uuid.New(), rawSHA([]byte("override")), uuid.New(), rawSHA([]byte("mask")),
		uuid.New(), rawSHA([]byte("pixels")), uuid.New(), rawSHA([]byte("receipt")), `{}`).Scan(&live); err != nil {
		t.Fatal(err)
	}
	if live {
		t.Fatal("absent verified receipt accepted")
	}
}
