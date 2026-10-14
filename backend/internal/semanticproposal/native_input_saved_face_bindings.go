package semanticproposal

import (
	"context"
	"encoding/json"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Saved manual declarations may be selected only for the exact independently
// verified original FAN/skin faces. This checks binding identity, not manual
// producer authority; each manual closure still needs physical and SQL checks.
func verifyNativeInputSavedFaceBindings(ctx context.Context, actual, expected []review.DefectProposalFaceInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if actual == nil || expected == nil || len(actual) > 64 || len(actual) != len(expected) {
		return v2input.ErrInvalid
	}
	raw, err := canonical.Marshal(actual)
	if err != nil || len(raw) > 1<<20 {
		return v2input.ErrInvalid
	}
	var owned []review.DefectProposalFaceInput
	if json.Unmarshal(raw, &owned) != nil {
		return v2input.ErrInvalid
	}
	for i := range owned {
		owned[i].Manual = nil
	}
	a, err := canonical.Hash(owned)
	if err != nil {
		return err
	}
	b, err := canonical.Hash(expected)
	if err != nil {
		return err
	}
	if a != b {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
