package semanticproposal

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type nativeSkinStageParams struct {
	document uuid.UUID
	ref      v2input.Ref
	parser   completedNativeFaceParse
}

// The completed parser reader checks physical NPY bytes and original producer
// authority on both sides of publication. This does not activate a skin task.
func (s SQLTransferSourceReader) publishNativeSkinStageParams(ctx context.Context, who auth.Identity, plan nativeFaceDraftPlan, color TransferColorProfile, publisher v2draft.Publisher) (nativeSkinStageParams, error) {
	var zero nativeSkinStageParams
	before, err := s.readCompletedNativeFaceParse(ctx, who, plan, color)
	if err != nil {
		return zero, err
	}
	document := uuid.NewSHA1(plan.detector.request.RunID, []byte("semantic.mask.face_skin.params:reference"))
	binding := plan.detector.request.Intent.Reference
	image, labels, parser, detector := json.RawMessage(before.detector.sourceBody), json.RawMessage(before.body), json.RawMessage(before.paramsBody), json.RawMessage(before.detector.body)
	prepared, err := v2input.PrepareSemanticFaceSkinParams(ctx, binding, image, before.catalog.Outputs[1], labels, before.catalog.Params, parser, before.detector.catalog.Output, detector)
	if err != nil {
		return zero, err
	}
	ref, err := publisher.PublishSemanticFaceSkinParams(ctx, who, plan.detector.request.TenantID, document, binding, image, before.catalog.Outputs[1], labels, before.catalog.Params, parser, before.detector.catalog.Output, detector)
	if err != nil {
		return zero, err
	}
	digest, err := canonical.Hash(prepared.Params)
	if err != nil || ref.ArtifactID == uuid.Nil || ref.SHA256 != digest {
		return zero, v2input.ErrConflict
	}
	after, err := s.readCompletedNativeFaceParse(ctx, who, plan, color)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(before, after) {
		return zero, v2input.ErrConflict
	}
	return nativeSkinStageParams{document: document, ref: ref, parser: before}, nil
}
