package semanticproposal

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

func TestNativeReferenceContinuationRejectsMixedParentPolicyAndPartialProofs(t *testing.T) {
	sha := strings.Repeat("a", 64)
	operations := []string{"defects.propose", "face.detect", "face.landmarks", "face.parse", "mask.person"}
	var rows []qualificationPolicyRow
	for _, op := range operations {
		rows = append(rows, qualificationPolicyRow{ID: uuid.New(), Operation: op, Version: "1", Model: sha, Runtime: sha, Device: sha, Dataset: sha, Backend: "cpu", Issuer: uuid.New(), Checks: []string{"quality"}, Fixtures: []string{sha}, Evidence: []string{sha}, License: true, Enabled: true, Admission: true})
	}
	body, err := canonical.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	policy := qualificationPolicySnapshot{body: string(body)}
	selected, err := selectQualificationPolicies(policy, operations)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := canonical.Marshal(selected["mask.person"])
	if err != nil {
		t.Fatal(err)
	}
	digest, err := canonical.Hash(json.RawMessage(body))
	if err != nil {
		t.Fatal(err)
	}
	parent := nativeDefectParentProof{anchor: "retained-original-anchor"}
	parent.result.task = uuid.New()
	parent.result.catalog.ResultID = uuid.New()
	protection := nativeReferenceProtectionProof{policy: policy, policySHA: digest}
	protection.selection.count = 1
	protection.selection.registered.parent = parent
	protection.person.registered = protection.selection.registered
	protection.person.expected = string(expected)
	defect := nativeDefectQualificationProof{verified: true, parent: parent, policy: policy, faceSet: "[]", parserSet: "[]"}
	proof, err := composeNativeReferenceContinuation(defect, protection)
	if err != nil {
		t.Fatal("consistent empty original producer declaration", err)
	}
	for _, mode := range []string{"parent", "policy", "digest", "person-parent", "face-count", "parser-count", "face-set", "parser-set", "unverified"} {
		changed := proof
		switch mode {
		case "parent":
			changed.defect.parent.result.catalog.ResultID = uuid.New()
		case "policy":
			changed.defect.policy.body = "[]"
		case "digest":
			changed.protection.policySHA = strings.Repeat("b", 64)
		case "person-parent":
			changed.protection.person.registered.parent.anchor = "foreign"
		case "face-count":
			changed.defect.faceCount = 1
		case "parser-count":
			changed.defect.parserCount = 1
		case "face-set":
			changed.defect.faceSet = ""
		case "parser-set":
			changed.defect.parserSet = ""
		case "unverified":
			changed.defect.verified = false
		}
		if err := matchNativeReferenceContinuation(changed); err == nil {
			t.Fatal("mixed native continuation accepted", mode)
		}
	}
}
