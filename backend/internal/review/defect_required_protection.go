package review

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

// Verifies native Params content retains every independently authorized required
// protected mask with its exact geometry, selector, object and provenance fields.
// Required masks must come from trusted preparation; this grants no SQL authority.
func VerifyDefectRequiredProtections(ctx context.Context, paramsRef ArtifactRef, raw []byte, source DefectSource, required []ProtectionMask) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if required == nil || len(required) > 65 {
		return errors.New("invalid required defect protections")
	}
	masks, err := DefectProposalMaskInputs(paramsRef, raw, source)
	if err != nil {
		return err
	}
	actual := map[uuid.UUID]string{}
	for _, mask := range masks {
		if !mask.Protected {
			continue
		}
		hash, err := canonical.Hash(mask)
		if err != nil {
			return err
		}
		if old, exists := actual[mask.Artifact.ID]; exists && old != hash {
			return errors.New("conflicting native defect protection")
		}
		actual[mask.Artifact.ID] = hash
	}
	for _, mask := range required {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !mask.Protected || !validMaskBinding(mask) {
			return errors.New("invalid required defect protection binding")
		}
		hash, err := canonical.Hash(mask)
		if err != nil {
			return err
		}
		if actual[mask.Artifact.ID] != hash {
			return errors.New("missing or changed required defect protection")
		}
	}
	return ctx.Err()
}
