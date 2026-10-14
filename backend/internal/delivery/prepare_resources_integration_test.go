package delivery

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Uses an isolated migrated database; applies no migrations and writes no rows.
func TestPrepareSQLResourceEstimateUsesIndexedInventory(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	const documentBytes int64 = 4096
	const count int64 = 1000
	base := prepareResourceEstimate{RAMBytes: (512 << 20) + 32*documentBytes + 1024*count, ScratchBytes: (32 << 20) + documentBytes + 1024*count, OutputBytes: 8 << 20}
	for _, kind := range []string{"exact", "ram", "scratch", "direct-only", "gpu", "output", "count-zero", "count-overflow", "document-overflow"} {
		t.Run(kind, func(t *testing.T) {
			estimate := base
			n, size := count, documentBytes
			switch kind {
			case "ram":
				estimate.RAMBytes--
			case "scratch":
				estimate.ScratchBytes--
			case "direct-only":
				estimate.RAMBytes = (512 << 20) + 32*documentBytes + 2048
			case "gpu":
				estimate.VRAMBytes = 1
			case "output":
				estimate.OutputBytes--
			case "count-zero":
				n = 0
			case "count-overflow":
				n = 1000001
			case "document-overflow":
				size = 27262977
			}
			raw, err := json.Marshal(estimate)
			if err != nil {
				t.Fatal(err)
			}
			var accepted bool
			if err := db.QueryRow(ctx, `SELECT photo.v30_delivery_prepare_resource_estimate_ready($1::jsonb,$2,$3)`, string(raw), size, n).Scan(&accepted); err != nil {
				t.Fatal(err)
			}
			if accepted != (kind == "exact") {
				t.Fatalf("%s accepted=%v", kind, accepted)
			}
		})
	}
}
