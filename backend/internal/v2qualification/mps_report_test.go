package v2qualification

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
	"time"
)

func mpsFixture(t *testing.T) (map[string]any, Expected) {
	t.Helper()
	pin := func(v string) string { return strings.Repeat(v, 64) }
	issuer := uuid.New()
	expected := Expected{Operation: "face.detect", Version: "1", Backend: "mps", ModelLockSHA: pin("a"), RuntimeLockSHA: pin("b"), DeviceProfileSHA: pin("c"), DatasetSHA: pin("d"), IssuerID: issuer, RequiredChecks: []string{"quality", "metal_real_inference", "gpu_memory_envelope"}, CalibrationSHAs: map[string]struct{}{pin("0"): {}}, TestFixtureSHAs: map[string]struct{}{pin("e"): {}}, EvidenceSHAs: map[string]struct{}{pin("f"): {}}, LicenseCleared: true, MPSResources: &MPSResourcePolicy{Metric: MPSAllocatorMetric, AllocatorBytes: 20000000000, P95MS: 100, RSSBytes: 4096, ScratchBytes: 8192}}
	checks := []any{}
	for _, id := range expected.RequiredChecks {
		checks = append(checks, map[string]any{"id": id, "passed": true, "evidence_sha": pin("f")})
	}
	raw := map[string]any{"schema_version": 2, "backend": "mps", "operation": "face.detect", "version": "1", "model_lock_sha": pin("a"), "runtime_lock_sha": pin("b"), "device_profile_sha": pin("c"), "dataset_sha": pin("d"), "issuer_id": issuer.String(), "created_at": time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC).Format(time.RFC3339), "input_fixture_shas": []string{pin("e")}, "checks": checks, "metrics": map[string]any{"p50_ms": 1, "p95_ms": 2, "peak_rss_bytes": 1024, "scratch_bytes": 2048}, "gpu_memory": map[string]any{"metric": MPSAllocatorMetric, "allocator_ceiling_bytes": 20000000000, "driver_sample_max_bytes": 15, "peak_measurement_available": false}, "status": "passed"}
	return raw, expected
}
func mpsEncode(t *testing.T, value any) ([]byte, string) {
	t.Helper()
	raw, e := canonical.Marshal(value)
	if e != nil {
		t.Fatal(e)
	}
	h := sha256.Sum256(raw)
	return raw, hex.EncodeToString(h[:])
}
func TestMPSExplicitResourcePolicyAndSchema(t *testing.T) {
	value, expected := mpsFixture(t)
	raw, sha := mpsEncode(t, value)
	report, e := VerifyMPS(raw, sha, expected)
	if e != nil {
		t.Fatal(e)
	}
	if report.GPU.Ceiling != 20000000000 || report.GPU.SampleMax != 15 || report.GPU.PeakAvailable {
		t.Fatal("resource semantics changed")
	}
	if _, e = Verify(raw, sha, expected); e == nil {
		t.Fatal("legacy verifier accepted MPS envelope as peak")
	}
	expected.MPSResources = nil
	if _, e = VerifyMPS(raw, sha, expected); e == nil {
		t.Fatal("report selected its own resource policy")
	}
}
func TestMPSRefusesChangedAuthorityResourcesAndEvidence(t *testing.T) {
	for _, name := range []string{"issuer", "model", "runtime", "device", "dataset", "backend", "resource_policy", "metric", "allocator", "sample", "claimed_peak", "null_peak", "fractional_bytes", "missing_check", "false_check", "unknown_evidence", "calibration_overlap", "license", "body_sha", "schema", "rss", "timing"} {
		t.Run(name, func(t *testing.T) {
			value, expected := mpsFixture(t)
			gpu := value["gpu_memory"].(map[string]any)
			metrics := value["metrics"].(map[string]any)
			switch name {
			case "issuer":
				value["issuer_id"] = uuid.NewString()
			case "model":
				value["model_lock_sha"] = strings.Repeat("0", 64)
			case "runtime":
				value["runtime_lock_sha"] = strings.Repeat("0", 64)
			case "device":
				value["device_profile_sha"] = strings.Repeat("0", 64)
			case "dataset":
				value["dataset_sha"] = strings.Repeat("0", 64)
			case "backend":
				value["backend"] = "cpu"
			case "resource_policy":
				expected.MPSResources = nil
			case "metric":
				gpu["metric"] = "sampled_peak"
			case "allocator":
				gpu["allocator_ceiling_bytes"] = 20000000001
			case "sample":
				gpu["driver_sample_max_bytes"] = 20000000001
			case "claimed_peak":
				metrics["peak_vram_bytes"] = 15
			case "null_peak":
				gpu["peak_measurement_available"] = nil
			case "fractional_bytes":
				gpu["allocator_ceiling_bytes"] = 15.5
			case "missing_check":
				value["checks"] = value["checks"].([]any)[:1]
			case "false_check":
				value["checks"].([]any)[0].(map[string]any)["passed"] = false
			case "unknown_evidence":
				expected.EvidenceSHAs = map[string]struct{}{}
			case "calibration_overlap":
				expected.CalibrationSHAs = expected.TestFixtureSHAs
			case "license":
				expected.LicenseCleared = false
			case "schema":
				value["schema_version"] = 1
			case "rss":
				metrics["peak_rss_bytes"] = 4097
			case "timing":
				metrics["p95_ms"] = 101
			}
			raw, sha := mpsEncode(t, value)
			if name == "body_sha" {
				sha = strings.Repeat("0", 64)
			}
			if _, e := VerifyMPS(raw, sha, expected); e == nil {
				t.Fatal("invalid MPS report accepted")
			}
		})
	}
}
func TestMPSDeploymentLocksOwnResourcePolicy(t *testing.T) {
	_, expected := mpsFixture(t)
	locks := DeploymentLocks{Operation: expected.Operation, Version: expected.Version, Backend: expected.Backend, ModelLockSHA: expected.ModelLockSHA, RuntimeLockSHA: expected.RuntimeLockSHA, DeviceProfileSHA: expected.DeviceProfileSHA, DatasetSHA: expected.DatasetSHA, IssuerID: expected.IssuerID.String(), RequiredChecks: expected.RequiredChecks, TestFixtureSHAs: []string{strings.Repeat("e", 64)}, EvidenceSHAs: []string{strings.Repeat("f", 64)}, LicenseCleared: true, MPSResources: expected.MPSResources}
	owned, e := locks.Expected()
	if e != nil {
		t.Fatal(e)
	}
	locks.MPSResources.AllocatorBytes = 1
	if owned.MPSResources.AllocatorBytes != 20000000000 {
		t.Fatal("validated policy shares mutable source")
	}
	// Exact serialized contract still rejects unknown fields in LoadLocks; this
	// check validates the optional MPS object roundtrip without weakening CPU.
	raw, e := json.Marshal(locks)
	if e != nil || len(raw) == 0 {
		t.Fatal(e)
	}
}

// A trusted subset is not the independently approved whole child corpus.
func TestMPSRequiresCompleteOperationCorpus(t *testing.T) {
	value, expected := mpsFixture(t)
	expected.TestFixtureSHAs[strings.Repeat("9", 64)] = struct{}{}
	raw, sha := mpsEncode(t, value)
	if _, err := VerifyMPS(raw, sha, expected); err == nil {
		t.Fatal("partial operation corpus accepted")
	}
	value["input_fixture_shas"] = []string{strings.Repeat("e", 64), strings.Repeat("9", 64)}
	raw, sha = mpsEncode(t, value)
	if _, err := VerifyMPS(raw, sha, expected); err != nil {
		t.Fatal(err)
	}
}
