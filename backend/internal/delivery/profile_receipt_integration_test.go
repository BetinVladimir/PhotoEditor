package delivery

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
)

// Execute the actual SQL predicate after migration in an isolated test database.
// These cases establish profile comparison only, not full delivery admission.
func TestDeliverySealedProfilePreservesIntentAcrossReceipt(t *testing.T) {
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
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_fn_owner"); err != nil {
		t.Fatal(err)
	}
	expected := map[string]any{"backend": "cpu", "renderer_version": "v2.2", "contracts_sha": "pinned", "resource_estimate": map[string]any{"vram_bytes": 0, "output_bytes": 8388608}}
	for _, scenario := range []string{"sealed", "unsealed", "backend", "resources", "extra", "caller_seal_fields"} {
		t.Run(scenario, func(t *testing.T) {
			actual := map[string]any{}
			for k, v := range expected {
				actual[k] = v
			}
			actual["v2_input_manifest"] = map[string]any{"artifact_id": "immutable", "sha256": "exact"}
			actual["v2_entry_count"] = 5
			actual["v2_entries_sha"] = "entries"
			intent := map[string]any{}
			for k, v := range expected {
				intent[k] = v
			}
			switch scenario {
			case "unsealed":
				delete(actual, "v2_entries_sha")
			case "backend":
				actual["backend"] = "mps"
			case "resources":
				actual["resource_estimate"] = map[string]any{"output_bytes": 1}
			case "extra":
				actual["unexpected"] = true
			case "caller_seal_fields":
				intent["v2_entry_count"] = 5
			}
			a, err := json.Marshal(actual)
			if err != nil {
				t.Fatal(err)
			}
			e, err := json.Marshal(intent)
			if err != nil {
				t.Fatal(err)
			}
			var matches bool
			if err := tx.QueryRow(ctx, `SELECT photo.v30_delivery_profile_matches($1::jsonb,$2::jsonb)`, string(a), string(e)).Scan(&matches); err != nil {
				t.Fatal(err)
			}
			if matches != (scenario == "sealed") {
				t.Fatal("profile intent comparison", scenario, matches)
			}
		})
	}
}

func TestAssemblySQLResourceGateRejectsUnderReservation(t *testing.T) {
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
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_fn_owner"); err != nil {
		t.Fatal(err)
	}
	const documents int64 = 4096
	const inputs int64 = 7
	const ram int64 = 134217728 + documents*16 + inputs*1024
	const scratch int64 = documents + 8388608
	for _, scenario := range []string{"exact", "larger", "ram", "scratch", "gpu", "output", "fraction", "missing", "extra", "too_many_inputs"} {
		t.Run(scenario, func(t *testing.T) {
			resource := map[string]any{"ram_bytes": ram, "scratch_bytes": scratch, "vram_bytes": 0, "output_bytes": 8388608, "pixel_count": 0}
			profile := map[string]any{"backend": "cpu", "renderer_version": "v2.2", "resource_estimate": resource}
			count := inputs
			switch scenario {
			case "larger":
				resource["ram_bytes"] = ram + 1024
			case "ram":
				resource["ram_bytes"] = ram - 1
			case "scratch":
				resource["scratch_bytes"] = scratch - 1
			case "gpu":
				resource["vram_bytes"] = 1
			case "output":
				resource["output_bytes"] = 1
			case "fraction":
				resource["ram_bytes"] = float64(ram) + 0.5
			case "missing":
				delete(resource, "scratch_bytes")
			case "extra":
				resource["unexpected"] = true
			case "too_many_inputs":
				count = 1000001
			}
			raw, err := json.Marshal(profile)
			if err != nil {
				t.Fatal(err)
			}
			var ready bool
			if err := tx.QueryRow(ctx, `SELECT photo.v30_delivery_assembly_resources_ready($1::jsonb,$2,$3)`, string(raw), documents, count).Scan(&ready); err != nil {
				t.Fatal(err)
			}
			if ready != (scenario == "exact" || scenario == "larger") {
				t.Fatal("resource gate mismatch", scenario, ready)
			}
		})
	}
}
