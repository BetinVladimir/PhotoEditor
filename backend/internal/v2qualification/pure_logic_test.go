package v2qualification

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestMeasuredMetricsReturnsTrueForMeasuredGPUPeak covers the return r.measured, true branch.
func TestMeasuredMetricsReturnsTrueForMeasuredGPUPeak(t *testing.T) {
	want := Metrics{P50MS: 5.0, P95MS: 10.0, PeakVRAMBytes: 512.5, PeakRSSBytes: 1024.0, ScratchBytes: 256.0}
	r := ResourceEvidence{GPUKind: MeasuredGPUPeak, measured: want}
	got, ok := r.MeasuredMetrics()
	if !ok {
		t.Fatal("MeasuredMetrics must return true for MeasuredGPUPeak")
	}
	if got != want {
		t.Fatalf("MeasuredMetrics returned wrong metrics: %+v", got)
	}
}

// TestMeasuredMetricsReturnsFalseForNonMeasured covers the false branch.
func TestMeasuredMetricsReturnsFalseForNonMeasured(t *testing.T) {
	r := ResourceEvidence{GPUKind: MPSAllocatorMetric}
	if _, ok := r.MeasuredMetrics(); ok {
		t.Fatal("MeasuredMetrics must return false for non-MeasuredGPUPeak kind")
	}
}

// TestMixedAuthorityValidateRejectsInvalidSchema covers the ErrUnqualified early return.
func TestMixedAuthorityValidateRejectsInvalidSchema(t *testing.T) {
	a := MixedAuthority{Schema: 0, Scope: "renderer.staged.mixed"}
	if err := a.Validate(uuid.New(), uuid.New()); err != ErrUnqualified {
		t.Fatalf("invalid schema must return ErrUnqualified, got %v", err)
	}
}

// TestMixedAuthorityValidateRejectsNilTenant covers uuid.Nil tenant guard.
func TestMixedAuthorityValidateRejectsNilTenant(t *testing.T) {
	tid := uuid.New()
	a := MixedAuthority{Schema: 1, Scope: "renderer.staged.mixed", Tenant: tid}
	if err := a.Validate(uuid.Nil, uuid.New()); err != ErrUnqualified {
		t.Fatalf("nil tenant must return ErrUnqualified, got %v", err)
	}
}

// TestMixedAuthorityChildNotFound covers the ErrUnqualified return path in Child.
func TestMixedAuthorityChildNotFound(t *testing.T) {
	pin := strings.Repeat("a", 64)
	a := MixedAuthority{Children: []MixedChild{
		{Operation: "face.detect", Version: "1", Backend: "mps", Model: pin},
	}}
	if _, err := a.Child("face.detect", "1", "mps", strings.Repeat("b", 64)); err != ErrUnqualified {
		t.Fatalf("wrong model must return ErrUnqualified, got %v", err)
	}
	if _, err := a.Child("nonexistent", "1", "mps", pin); err != ErrUnqualified {
		t.Fatalf("unknown operation must return ErrUnqualified, got %v", err)
	}
}

// TestDigestRejectsNonHexChars covers the inner return false branch in digest.
func TestDigestRejectsNonHexChars(t *testing.T) {
	// 'G' is above 'f', triggering the inner c < 'a' || c > 'f' guard
	invalidHex := strings.Repeat("G", 64)
	if digest(invalidHex) {
		t.Fatal("uppercase hex must be rejected")
	}
	// ' ' is below '0', triggers the outer guard and then the inner guard
	withSpace := strings.Repeat(" ", 64)
	if digest(withSpace) {
		t.Fatal("spaces must be rejected")
	}
	// valid lowercase hex must pass
	if !digest(strings.Repeat("a", 64)) {
		t.Fatal("valid lowercase hex must be accepted")
	}
	// mix of digits and letters must pass
	if !digest(strings.Repeat("0", 32) + strings.Repeat("f", 32)) {
		t.Fatal("valid mixed hex must be accepted")
	}
}

// TestTrustedSetRejectsDuplicatesAndInvalidDigests covers the false branches.
func TestTrustedSetRejectsDuplicatesAndInvalidDigests(t *testing.T) {
	good := strings.Repeat("a", 64)
	// duplicate
	if _, ok := trustedSet([]string{good, good}, 1); ok {
		t.Fatal("duplicate digest must be rejected")
	}
	// invalid digest
	if _, ok := trustedSet([]string{"notahex"}, 1); ok {
		t.Fatal("invalid digest must be rejected")
	}
	// too few values
	if _, ok := trustedSet([]string{good}, 2); ok {
		t.Fatal("below min must be rejected")
	}
	// too many values (>10000) — build a slice of 10001 distinct valid-looking digests
	// (use first as good, rest as variations — we just need len >10000 with any values)
	large := make([]string, 10001)
	for i := range large {
		// each must be distinct and 64-char hex — vary last 4 hex chars
		large[i] = strings.Repeat("0", 60) + strings.ReplaceAll(strings.ToLower(strings.Repeat("0", 4)), "", "")
	}
	// simpler: just test length > 10000 with obviously invalid content (len check fires first)
	largeSlice := make([]string, 10001)
	if _, ok := trustedSet(largeSlice, 1); ok {
		t.Fatal("over-10000 must be rejected")
	}
	// valid single-element set
	if s, ok := trustedSet([]string{good}, 1); !ok || len(s) != 1 {
		t.Fatal("valid single-element set rejected")
	}
}

// TestExactObjectRejectsInvalidJSON covers json.Unmarshal error and field count mismatch.
func TestExactObjectRejectsInvalidJSON(t *testing.T) {
	// invalid JSON
	if exactObject([]byte("not json"), []string{"a"}) {
		t.Fatal("invalid JSON must be rejected")
	}
	// field count mismatch (too many)
	if exactObject([]byte(`{"a":1,"b":2}`), []string{"a"}) {
		t.Fatal("extra field must be rejected")
	}
	// missing required field
	if exactObject([]byte(`{"a":1}`), []string{"a", "b"}) {
		t.Fatal("missing field must be rejected")
	}
	// metrics sub-object with unknown field
	bad := []byte(`{"schema_version":1,"operation":"x","version":"y","model_lock_sha":"m","runtime_lock_sha":"r","device_profile_sha":"d","dataset_sha":"ds","input_fixture_shas":[],"checks":[],"metrics":{"p50_ms":1,"p95_ms":2,"peak_vram_bytes":3,"peak_rss_bytes":4,"scratch_bytes":5,"extra_field":6},"status":"passed","issuer_id":"i","created_at":"t"}`)
	names := []string{"schema_version","operation","version","model_lock_sha","runtime_lock_sha","device_profile_sha","dataset_sha","input_fixture_shas","checks","metrics","status","issuer_id","created_at"}
	if exactObject(bad, names) {
		t.Fatal("metrics with extra field must be rejected")
	}
}

// TestResolveOperatorMixedChildrenNilDBGuard covers the nil-DB early return.
func TestResolveOperatorMixedChildrenNilDBGuard(t *testing.T) {
	pin := strings.Repeat("a", 64)
	ex := MixedExpected{Common: Expected{DatasetSHA: pin}}
	if _, err := ResolveOperatorMixedChildren(nil, nil, uuid.New(), uuid.New(), uuid.New(), uuid.New(), ex); err != ErrUnqualified {
		t.Fatalf("nil db must return ErrUnqualified, got %v", err)
	}
}
