package review

import (
	"context"
	"errors"

	"photoeditor/backend/internal/canonical"
)

// Native preparation must preserve the complete ordered face bindings from its
// independently qualified original producer evidence. This does not establish
// producer authority or grant queue admission.
func VerifyDefectQualifiedFaceBindings(ctx context.Context, paramsRef ArtifactRef, raw []byte, source DefectSource, qualified []DefectProposalFaceInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if qualified == nil || len(qualified) > 64 {
		return errors.New("invalid qualified defect face inventory")
	}
	faces, err := DefectProposalFaceInputsFromParams(paramsRef, raw, source)
	if err != nil {
		return err
	}
	if len(faces) != len(qualified) {
		return errors.New("changed qualified defect face coverage")
	}
	for i, face := range faces {
		if err := ctx.Err(); err != nil {
			return err
		}
		actual, err := canonical.Hash(face)
		if err != nil {
			return err
		}
		expected, err := canonical.Hash(qualified[i])
		if err != nil {
			return err
		}
		if actual != expected {
			return errors.New("changed qualified defect face binding")
		}
	}
	return ctx.Err()
}
