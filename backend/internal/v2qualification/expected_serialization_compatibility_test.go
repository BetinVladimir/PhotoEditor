package v2qualification

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
)

type preMPSExpectedSerialization struct {
	Operation, Version, Backend                                string
	ModelLockSHA, RuntimeLockSHA, DeviceProfileSHA, DatasetSHA string
	IssuerID                                                   uuid.UUID
	RequiredChecks                                             []string
	CalibrationSHAs                                            map[string]struct{}
	TestFixtureSHAs                                            map[string]struct{}
	EvidenceSHAs                                               map[string]struct{}
	LicenseCleared                                             bool
}

func legacyExpectedSerializationFixture() Expected {
	set := func(s string) map[string]struct{} { return map[string]struct{}{strings.Repeat(s, 64): {}} }
	return Expected{Operation: "face.detect", Version: "1", Backend: "cuda", ModelLockSHA: strings.Repeat("a", 64), RuntimeLockSHA: strings.Repeat("b", 64), DeviceProfileSHA: strings.Repeat("c", 64), DatasetSHA: strings.Repeat("d", 64), IssuerID: uuid.MustParse("11111111-2222-4333-8444-555555555555"), RequiredChecks: []string{"quality"}, CalibrationSHAs: set("0"), TestFixtureSHAs: set("e"), EvidenceSHAs: set("f"), LicenseCleared: true}
}
func TestExpectedNilMPSResourcesRetainsFrozenLegacyJSONAndHash(t *testing.T) {
	expected := legacyExpectedSerializationFixture()
	// This independent original shape is frozen from git HEAD before MPS existed.
	original := preMPSExpectedSerialization{Operation: expected.Operation, Version: expected.Version, Backend: expected.Backend, ModelLockSHA: expected.ModelLockSHA, RuntimeLockSHA: expected.RuntimeLockSHA, DeviceProfileSHA: expected.DeviceProfileSHA, DatasetSHA: expected.DatasetSHA, IssuerID: expected.IssuerID, RequiredChecks: expected.RequiredChecks, CalibrationSHAs: expected.CalibrationSHAs, TestFixtureSHAs: expected.TestFixtureSHAs, EvidenceSHAs: expected.EvidenceSHAs, LicenseCleared: expected.LicenseCleared}
	const frozen = `{"Backend":"cuda","CalibrationSHAs":{"0000000000000000000000000000000000000000000000000000000000000000":{}},"DatasetSHA":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd","DeviceProfileSHA":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","EvidenceSHAs":{"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff":{}},"IssuerID":"11111111-2222-4333-8444-555555555555","LicenseCleared":true,"ModelLockSHA":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","Operation":"face.detect","RequiredChecks":["quality"],"RuntimeLockSHA":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","TestFixtureSHAs":{"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee":{}},"Version":"1"}`
	const frozenSHA = "9c23ea4c24570c58db6a39ba63f2091887be345bc45f9c68acdbad3033f4f203"
	before, err := canonical.Marshal(original)
	if err != nil || string(before) != frozen {
		t.Fatal("legacy fixture drift", err)
	}
	raw, err := canonical.Marshal(expected)
	if err != nil || string(raw) != frozen {
		t.Fatal("nil policy changed legacy canonical bytes", err)
	}
	sha, err := canonical.Hash(expected)
	if err != nil || sha != frozenSHA {
		t.Fatal("legacy policy hash changed", sha, err)
	}
	for _, backend := range []string{"cpu", "cuda"} {
		expected.Backend = backend
		original.Backend = backend
		a, err := canonical.Marshal(expected)
		if err != nil {
			t.Fatal(err)
		}
		b, err := canonical.Marshal(original)
		if err != nil || string(a) != string(b) {
			t.Fatal("legacy backend bytes changed", backend, err)
		}
	}
}
func TestExpectedNonNilMPSResourcesKeepsUppercaseFieldAndOwnedRoundtrip(t *testing.T) {
	expected := legacyExpectedSerializationFixture()
	expected.Backend = "mps"
	expected.MPSResources = &MPSResourcePolicy{Metric: MPSAllocatorMetric, AllocatorBytes: 20000000000, P95MS: 100, RSSBytes: 4096, ScratchBytes: 8192}
	raw, err := canonical.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		t.Fatal("invalid canonical JSON")
	}
	if _, ok := fields["MPSResources"]; !ok {
		t.Fatal("MPS policy erased or renamed")
	}
	if _, ok := fields["mps_resources"]; ok {
		t.Fatal("Expected key was renamed")
	}
	var policy MPSResourcePolicy
	if json.Unmarshal(fields["MPSResources"], &policy) != nil || policy != *expected.MPSResources {
		t.Fatal("MPS resource fields changed")
	}
	var decoded Expected
	if json.Unmarshal(raw, &decoded) != nil || decoded.MPSResources == nil || *decoded.MPSResources != *expected.MPSResources || decoded.Backend != "mps" {
		t.Fatal("MPS policy roundtrip lost authority")
	}
	decoded.MPSResources.AllocatorBytes = 1
	if expected.MPSResources.AllocatorBytes != 20000000000 {
		t.Fatal("roundtrip aliases expected")
	}
}
