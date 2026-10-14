package v2portrait

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// This checks the installed migration, rather than merely parsing its SQL.
// Output role counts must match the operation contract even when the current
// donor worker emits only a subset of the optional roles.
func TestIntegrationOpenEyesRegistry(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PHOTO_TEST_DATABASE_URL and migrations through 01958 required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(context.Background())
	var route, resource string
	var enabled bool
	if err := db.QueryRow(ctx, `SELECT routing_key,resource_class,v2_enabled
		FROM photo.operation_registry WHERE operation='portrait.open_eyes' AND version='1'`).Scan(&route, &resource, &enabled); err != nil {
		t.Fatal(err)
	}
	if route != "cpu.portrait_open_eyes" || resource != "cpu" || enabled {
		t.Fatalf("unexpected open-eyes registry: route=%q resource=%q enabled=%v", route, resource, enabled)
	}
	type policy struct {
		count int
		bytes int64
		media string
	}
	want := map[string]policy{
		"document":      {1, 8388608, `application/json`},
		"working_image": {1, 8388608, `application/json`},
		"pixels":        {1, 2400004096, `application/x-npy`},
		"report":        {1, 8388608, `application/json`},
		"mask":          {2, 8388608, `application/json`},
		"mask_pixels":   {2, 800004096, `application/x-npy`},
		"transform":     {1, 8388608, `application/json`},
		"registration":  {30, 8388608, `application/json`},
		"alpha":         {1, 800004096, `application/x-npy`},
		"warp_field":    {32, 1600004096, `application/x-npy`},
	}
	rows, err := db.Query(ctx, `SELECT role,max_count,max_bytes,media_types::text FROM photo.operation_output_policies
		WHERE operation='portrait.open_eyes' AND version='1'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var role string
		var count int
		var bytes int64
		var media string
		if err := rows.Scan(&role, &count, &bytes, &media); err != nil {
			t.Fatal(err)
		}
		policy, known := want[role]
		if seen[role] || !known || policy.count != count || policy.bytes != bytes || media != `["`+policy.media+`"]` {
			t.Fatalf("unexpected output policy: role=%q count=%d bytes=%d media=%q", role, count, bytes, media)
		}
		seen[role] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(seen) != len(want) {
		t.Fatalf("open-eyes policies: got %d roles, want %d", len(seen), len(want))
	}
}
