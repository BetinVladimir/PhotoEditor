package v2qualification

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Exercise the installed owner-only predicate with independent legacy JSON,
// rather than using Expected serialization to manufacture both sides equally.
func TestExpectedPolicySQLLegacyAndMPSCompatibility(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated fully migrated PostgreSQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, "SET LOCAL ROLE photo_fn_owner"); err != nil {
		t.Fatal(err)
	}
	type fixture struct {
		evidence map[string]any
		policies []map[string]any
		expected map[string]map[string]any
	}
	makeFixture := func(backend string) fixture {
		f := fixture{expected: map[string]map[string]any{}}
		for _, op := range []string{"defects.propose", "face.landmarks", "face.detect", "face.parse", "mask.person"} {
			policyBackend := backend
			if op == "defects.propose" {
				policyBackend = "cpu"
			}
			policy := map[string]any{"operation": op, "operation_version": "1", "backend": policyBackend, "enabled": true, "admission_enabled": true, "license_cleared": true, "model_lock_sha": strings.Repeat("a", 64), "runtime_lock_sha": strings.Repeat("b", 64), "device_profile_sha": strings.Repeat("c", 64), "dataset_sha": strings.Repeat("d", 64), "issuer_id": "11111111-2222-4333-8444-555555555555", "required_checks": []string{"quality"}, "test_fixture_shas": []string{strings.Repeat("e", 64)}, "calibration_shas": []string{}, "evidence_shas": []string{strings.Repeat("f", 64)}}
			expected := map[string]any{"Operation": op, "Version": "1", "Backend": policyBackend, "ModelLockSHA": policy["model_lock_sha"], "RuntimeLockSHA": policy["runtime_lock_sha"], "DeviceProfileSHA": policy["device_profile_sha"], "DatasetSHA": policy["dataset_sha"], "IssuerID": policy["issuer_id"], "LicenseCleared": true, "RequiredChecks": []string{"quality"}, "TestFixtureSHAs": map[string]any{strings.Repeat("e", 64): map[string]any{}}, "CalibrationSHAs": map[string]any{}, "EvidenceSHAs": map[string]any{strings.Repeat("f", 64): map[string]any{}}}
			if policyBackend == "mps" {
				resources := map[string]any{"metric": "pytorch_allocator_conservative_ceiling", "allocator_ceiling_bytes": 20000000000, "p95_ms": 100, "peak_rss_bytes": 4096, "scratch_bytes": 8192}
				policy["mps_resource_policy"] = resources
				expected["MPSResources"] = resources
			}
			f.policies = append(f.policies, policy)
			f.expected[op] = expected
		}
		faces := []any{map[string]any{"expected": []any{f.expected["face.landmarks"], f.expected["face.detect"]}, "policies": []any{f.expected["face.landmarks"], f.expected["face.detect"]}}}
		parsers := []any{map[string]any{"expected": []any{f.expected["face.parse"], f.expected["face.detect"]}, "policies": []any{f.expected["face.parse"], f.expected["face.detect"]}}}
		f.evidence = map[string]any{"source_authority": map[string]any{"policy": f.policies, "expected": f.expected["defects.propose"]}, "person": map[string]any{"expected": f.expected["mask.person"]}, "faces": faces, "parser": map[string]any{"parsers": parsers}}
		return f
	}
	cases := []struct {
		name, backend string
		want          bool
		mutate        func(fixture)
	}{
		{"legacy_cpu", "cpu", true, nil}, {"legacy_cuda", "cuda", true, nil},
		{"explicit_null_policy_keeps_legacy_shape", "cuda", true, func(f fixture) {
			for _, p := range f.policies {
				p["mps_resource_policy"] = nil
			}
		}},
		{"interim_null_expected_not_legacy", "cuda", false, func(f fixture) { f.expected["mask.person"]["MPSResources"] = nil }},
		{"required_checks_drift", "cuda", false, func(f fixture) { f.expected["face.detect"]["RequiredChecks"] = []string{"other"} }},
		{"mps_allocator_policy", "mps", true, nil},
		{"mps_expected_policy_missing", "mps", false, func(f fixture) { delete(f.expected["face.detect"], "MPSResources") }},
		{"mps_expected_ceiling_changed", "mps", false, func(f fixture) {
			f.expected["mask.person"]["MPSResources"] = map[string]any{"metric": "pytorch_allocator_conservative_ceiling", "allocator_ceiling_bytes": 1, "p95_ms": 100, "peak_rss_bytes": 4096, "scratch_bytes": 8192}
		}},
		{"mps_invalid_metric_on_both_sides", "mps", false, func(f fixture) { f.policies[1]["mps_resource_policy"].(map[string]any)["metric"] = "measured_peak" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := makeFixture(c.backend)
			if c.mutate != nil {
				c.mutate(f)
			}
			raw, err := json.Marshal(f.evidence)
			if err != nil {
				t.Fatal(err)
			}
			var got bool
			if err = tx.QueryRow(ctx, `SELECT photo.semantic_defect_policy_expectations_match($1::jsonb)`, string(raw)).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Fatalf("predicate=%v want=%v", got, c.want)
			}
		})
	}
}
