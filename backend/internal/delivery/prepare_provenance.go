package delivery

import (
	"encoding/json"

	"photoeditor/backend/internal/v2input"
)

// ProvenanceMode reads the immutable frozen policy carried by the prepared
// parent. Runtime stage selection must use this value, not a caller option.
func (p PreparePlan) ProvenanceMode() (string, error) {
	var proof PrepareProof
	var frozen FrozenInput
	if json.Unmarshal(p.proofJCS, &proof) != nil || proof.SchemaVersion != 1 ||
		json.Unmarshal([]byte(proof.FrozenInputJCS), &frozen) != nil ||
		frozen.SchemaVersion != 1 || !validDeliverySigningPolicyChoice(frozen.Policy) {
		return "", v2input.ErrInvalid
	}
	return frozen.Policy.Provenance, nil
}
