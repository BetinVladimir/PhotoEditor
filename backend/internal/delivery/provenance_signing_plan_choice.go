package delivery

import (
	"encoding/json"

	"photoeditor/backend/internal/v2input"
)

// SigningChoice comes only from the frozen prepare proof, never the request
// or a scheduler configuration that could select a different policy.
func (p PreparePlan) SigningChoice() (FrozenSigningChoice, error) {
	var proof PrepareProof
	var frozen FrozenInput
	if len(p.proofJCS) < 2 || json.Unmarshal(p.proofJCS, &proof) != nil ||
		json.Unmarshal([]byte(proof.FrozenInputJCS), &frozen) != nil {
		return FrozenSigningChoice{}, v2input.ErrInvalid
	}
	return DeliverySigningChoice(frozen)
}
