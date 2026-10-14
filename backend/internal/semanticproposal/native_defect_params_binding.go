package semanticproposal

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Checks the exact compiled Params against retained original native face proofs.
// Manual/geometry roots may be declared, but require their separate authority.
func verifyNativeDefectPreparedParams(ctx context.Context, prepared nativeDefectFacePreparation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(prepared.landmarks.catalog.Outputs) != 1 {
		return v2input.ErrInvalid
	}
	p := prepared.params
	if len(p.params) < 2 || len(p.params) > 1<<20 || len(p.inputs) > 4<<20 || len(p.roots) > 4<<20 {
		return v2input.ErrInvalid
	}
	image, err := review.ParseWorkingImage([]byte(prepared.landmarks.detector.sourceBody))
	if err != nil {
		return err
	}
	original := prepared.landmarksPlan.detector.request.Intent.Reference.Source
	source := image.Source(review.ArtifactRef{ID: original.ArtifactID, SHA256: original.SHA256})
	var inputs, roots []review.ArtifactRef
	if json.Unmarshal([]byte(p.inputs), &inputs) != nil || json.Unmarshal([]byte(p.roots), &roots) != nil || roots == nil {
		return v2input.ErrInvalid
	}
	paramsID := uuid.NewSHA1(prepared.landmarksPlan.detector.request.RunID, []byte("semantic.defect.params:reference"))
	if err := review.VerifyDefectProposalDeclaredInputs(review.ArtifactRef{ID: paramsID, SHA256: p.digest}, []byte(p.params), source, inputs); err != nil {
		return err
	}
	expected, err := bindNativeDefectFaceInputs(ctx, prepared.landmarks.faces, review.ArtifactRef{ID: prepared.landmarks.catalog.Outputs[0].ArtifactID, SHA256: prepared.landmarks.catalog.Outputs[0].SHA256}, prepared.skin.mask, prepared.skin.parser.labels.Image)
	if err != nil {
		return err
	}
	var actual struct {
		Faces []review.DefectProposalFaceInput `json:"faces"`
	}
	if json.Unmarshal([]byte(p.params), &actual) != nil {
		return v2input.ErrInvalid
	}
	a, err := canonical.Hash(actual.Faces)
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
	for _, root := range roots {
		found := false
		for _, input := range inputs {
			if input == root {
				found = true
				break
			}
		}
		if !found {
			return v2input.ErrConflict
		}
	}
	return verifyNativeDefectPersonInput(ctx, prepared)
}
