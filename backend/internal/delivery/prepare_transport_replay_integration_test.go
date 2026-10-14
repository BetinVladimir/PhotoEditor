package delivery

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/v2input"
)

// Tests the exact profile comparison used by blocked transport replay. A full
// materialization/closure/quota fixture remains a separate integration case.
func TestPrepareTransportReplayPreservesFrozenProfile(t *testing.T) {
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
	expected := map[string]any{"backend": "cpu", "renderer_version": "v2.2", "resource_estimate": map[string]any{"ram_bytes": 800000000, "scratch_bytes": 40000000, "output_bytes": 8388608, "vram_bytes": 0, "pixel_count": 0}}
	for _, kind := range []string{"sealed", "missing-manifest", "renderer", "backend", "extra", "count-missing", "sha-missing", "contracts-missing", "contracts-invalid"} {
		t.Run(kind, func(t *testing.T) {
			raw, err := json.Marshal(expected)
			if err != nil {
				t.Fatal(err)
			}
			actual := map[string]any{}
			if err := json.Unmarshal(raw, &actual); err != nil {
				t.Fatal(err)
			}
			actual["v2_input_manifest"] = v2input.Ref{ArtifactID: uuid.New(), SHA256: digest([]byte("manifest"))}
			actual["contracts_sha"] = digest([]byte("contracts"))
			actual["v2_entry_count"] = 20
			actual["v2_entries_sha"] = digest([]byte("entries"))
			switch kind {
			case "missing-manifest":
				delete(actual, "v2_input_manifest")
			case "renderer":
				actual["renderer_version"] = "v2.3"
			case "backend":
				actual["backend"] = "cuda"
			case "extra":
				actual["unexpected"] = true
			case "count-missing":
				delete(actual, "v2_entry_count")
			case "sha-missing":
				delete(actual, "v2_entries_sha")
			case "contracts-missing":
				delete(actual, "contracts_sha")
			case "contracts-invalid":
				actual["contracts_sha"] = "invalid"
			}
			body, err := json.Marshal(actual)
			if err != nil {
				t.Fatal(err)
			}
			var accepted bool
			if err := db.QueryRow(ctx, `SELECT photo.v30_delivery_profile_matches($1::jsonb,$2::jsonb)`, string(body), string(raw)).Scan(&accepted); err != nil {
				t.Fatal(err)
			}
			if accepted != (kind == "sealed") {
				t.Fatalf("%s accepted=%v", kind, accepted)
			}
		})
	}
}
