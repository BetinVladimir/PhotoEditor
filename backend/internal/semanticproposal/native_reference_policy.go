package semanticproposal

import (
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Required families and the original independent person expectation must match
// one retained policy snapshot, including before any transactional SQL work.
func matchNativeReferencePolicy(proof nativeReferenceProtectionProof) error {
	sha, err := canonical.Hash(json.RawMessage(proof.policy.body))
	if err != nil || sha != proof.policySHA {
		return v2input.ErrInvalid
	}
	selected, err := selectNativeQualificationPolicies(proof.policy, []string{"defects.propose", "face.detect", "face.landmarks", "face.parse", "mask.person"})
	if err != nil {
		return err
	}
	var expected v2qualification.Expected
	if json.Unmarshal([]byte(proof.person.expected), &expected) != nil || !sameQualificationPolicyExpected(selected["mask.person"], expected) {
		return v2qualification.ErrUnqualified
	}
	return nil
}
