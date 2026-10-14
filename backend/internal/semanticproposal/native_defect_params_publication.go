package semanticproposal

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Publishes exact prepared native Params through accepted-version fencing/quota.
// Original producers are reread before and after publication. Manual/protection
// authority, defects qualification and admission remain independent gates.
func (s SQLTransferSourceReader) publishNativeDefectVersionParams(ctx context.Context, who auth.Identity, prepared nativeDefectFacePreparation, color TransferColorProfile, publisher v2draft.Publisher) (PublishedDefectParams, error) {
	var zero PublishedDefectParams
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if err := verifyNativeFacePlanSeal(ctx, prepared.landmarksPlan); err != nil {
		return zero, err
	}
	if err := verifyNativeSkinPlanSeal(ctx, prepared.skinPlan); err != nil {
		return zero, err
	}
	if prepared.landmarksPlan.detector.seal != prepared.skinPlan.parser.detector.seal {
		return zero, v2input.ErrConflict
	}
	if err := verifyNativeDefectPreparedParams(ctx, prepared); err != nil {
		return zero, err
	}
	recheck := func() error {
		faces, err := s.readCompletedNativeLandmarks(ctx, who, prepared.landmarksPlan, color)
		if err != nil {
			return err
		}
		skin, err := s.readCompletedNativeSkin(ctx, who, prepared.skinPlan, color)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(faces, prepared.landmarks) || !reflect.DeepEqual(skin, prepared.skin) {
			return v2input.ErrConflict
		}
		return s.recheckNativeDefectPersonInput(ctx, who, prepared)
	}
	if err := recheck(); err != nil {
		return zero, err
	}
	var inputs, roots []review.ArtifactRef
	if json.Unmarshal([]byte(prepared.params.inputs), &inputs) != nil || json.Unmarshal([]byte(prepared.params.roots), &roots) != nil {
		return zero, v2input.ErrInvalid
	}
	r := prepared.landmarksPlan.detector.request
	document := uuid.NewSHA1(r.RunID, []byte("semantic.defect.params:reference"))
	for _, input := range inputs {
		if input.ID == document {
			return zero, v2input.ErrConflict
		}
	}
	ref, err := publisher.PublishDefectProposalParams(ctx, who, r.TenantID, document, r.Intent.Reference, []byte(prepared.landmarks.detector.sourceBody), []byte(prepared.params.params), inputs)
	if err != nil {
		return zero, err
	}
	if ref.ArtifactID == uuid.Nil || ref.SHA256 != prepared.params.digest {
		return zero, v2input.ErrConflict
	}
	for _, input := range inputs {
		if input.ID == ref.ArtifactID {
			return zero, v2input.ErrConflict
		}
	}
	if err := recheck(); err != nil {
		return zero, err
	}
	return PublishedDefectParams{Ref: ref, Inputs: inputs, ManualRoots: roots}, nil
}
