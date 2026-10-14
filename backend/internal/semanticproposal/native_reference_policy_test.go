package semanticproposal

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
)

func TestNativeReferencePolicyRejectsMixedExpectedAndRevokedFamily(t *testing.T) {
	sha := strings.Repeat("a", 64)
	operations := []string{"defects.propose", "face.detect", "face.landmarks", "face.parse", "mask.person"}
	var rows []qualificationPolicyRow
	for _, operation := range operations {
		rows = append(rows, qualificationPolicyRow{ID: uuid.New(), Operation: operation, Version: "1", Model: sha, Runtime: sha, Device: sha, Dataset: sha, Backend: "cpu", Issuer: uuid.New(), Checks: []string{"quality"}, Fixtures: []string{sha}, Evidence: []string{sha}, License: true, Enabled: true, Admission: true})
	}
	body, _ := canonical.Marshal(rows)
	policy := qualificationPolicySnapshot{body: string(body)}
	selected, err := selectQualificationPolicies(policy, operations)
	if err != nil {
		t.Fatal(err)
	}
	expected, _ := canonical.Marshal(selected["mask.person"])
	digest, _ := canonical.Hash(json.RawMessage(body))
	proof := nativeReferenceProtectionProof{policy: policy, policySHA: digest, person: nativePersonProtectionProof{expected: string(expected)}}
	if err := matchNativeReferencePolicy(proof); err != nil {
		t.Fatal(err)
	}
	changed := proof
	person := ownQualificationExpected(selected["mask.person"])
	person.RuntimeLockSHA = strings.Repeat("b", 64)
	foreign, _ := canonical.Marshal(person)
	changed.person.expected = string(foreign)
	if err := matchNativeReferencePolicy(changed); err == nil {
		t.Fatal("foreign person policy accepted")
	}
	changed = proof
	changed.policySHA = strings.Repeat("0", 64)
	if err := matchNativeReferencePolicy(changed); err == nil {
		t.Fatal("changed original digest accepted")
	}
	for i := range rows {
		revoked := append([]qualificationPolicyRow(nil), rows...)
		revoked[i].Enabled = false
		raw, _ := canonical.Marshal(revoked)
		changed = proof
		changed.policy.body = string(raw)
		changed.policySHA, _ = canonical.Hash(json.RawMessage(raw))
		if err := matchNativeReferencePolicy(changed); err == nil {
			t.Fatal("revoked family accepted", rows[i].Operation)
		}
	}
}
