package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Reads the exact completed target SFD before and after immutable publication.
// This is preparatory Params evidence; downstream qualification, resource profile
// and locked task admission must still be performed separately.
func (s SQLTransferSourceReader) publishNativeTargetFaceStageParams(ctx context.Context, who auth.Identity, plan nativeTargetDetectorPlan, color TransferColorProfile, publisher v2draft.Publisher, operation string) (nativeFaceStageParams, error) {
	var zero nativeFaceStageParams
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if operation != "face.landmarks" && operation != "face.parse" {
		return zero, v2input.ErrInvalid
	}
	if err := verifyNativeTargetDetectorPlan(ctx, plan); err != nil {
		return zero, err
	}
	selected, err := selectQualificationPolicies(plan.detector.policy, []string{operation})
	if err != nil {
		return zero, err
	}
	model := selected[operation]
	if model.Backend != "cuda" {
		return zero, v2qualification.ErrUnqualified
	}
	before, err := s.readCompletedNativeTargetDetector(ctx, who, plan, color)
	if err != nil {
		return zero, err
	}
	document := uuid.NewSHA1(plan.anchor.entry.Namespace, []byte("semantic."+operation+".params:reference"))
	binding := plan.anchor.entry.Binding
	image, faces, detector := json.RawMessage(before.sourceBody), json.RawMessage(before.body), json.RawMessage(before.detectorParamsBody)
	var body json.RawMessage
	var ref v2input.Ref
	if operation == "face.landmarks" {
		prepared, prepareErr := v2input.PrepareSemanticLandmarkParams(ctx, binding, image, before.catalog.Output, faces, before.catalog.Params, detector, model.ModelLockSHA)
		if prepareErr != nil {
			return zero, prepareErr
		}
		body = prepared.Params
		ref, err = publisher.PublishSemanticLandmarkParams(ctx, who, plan.detector.request.TenantID, document, binding, image, before.catalog.Output, faces, before.catalog.Params, detector, model.ModelLockSHA)
	} else {
		prepared, prepareErr := v2input.PrepareSemanticFaceParseParams(ctx, binding, image, before.catalog.Output, faces, before.catalog.Params, detector, model.ModelLockSHA)
		if prepareErr != nil {
			return zero, prepareErr
		}
		body = prepared.Params
		ref, err = publisher.PublishSemanticFaceParseParams(ctx, who, plan.detector.request.TenantID, document, binding, image, before.catalog.Output, faces, before.catalog.Params, detector, model.ModelLockSHA)
	}
	if err != nil {
		return zero, err
	}
	digest, err := canonical.Hash(body)
	if err != nil || ref.ArtifactID == uuid.Nil || ref.SHA256 != digest {
		return zero, v2input.ErrConflict
	}
	after, err := s.readCompletedNativeTargetDetector(ctx, who, plan, color)
	if err != nil {
		return zero, err
	}
	if before.catalog != after.catalog || before.body != after.body || before.sourceBody != after.sourceBody || before.detectorParamsBody != after.detectorParamsBody {
		return zero, v2input.ErrConflict
	}
	return nativeFaceStageParams{operation: operation, document: document, ref: ref, detector: before.catalog, sourceBody: before.sourceBody, facesBody: before.body, detectorParamsBody: before.detectorParamsBody}, nil
}
