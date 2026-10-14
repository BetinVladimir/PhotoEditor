package semanticproposal

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Verify target immutable FAN bytes before downstream use. This preflight
// does not replace transaction locks or authorize skin/defect admission.
func (s SQLTransferSourceReader) readCompletedNativeTargetLandmarks(ctx context.Context, who auth.Identity, plan nativeTargetFaceDraftPlan, color TransferColorProfile) (completedNativeLandmarks, error) {
	var zero completedNativeLandmarks
	if err := verifyNativeTargetFacePlanSeal(ctx, plan); err != nil {
		return zero, err
	}
	if plan.prepared.operation != "face.landmarks" {
		return zero, v2input.ErrInvalid
	}
	catalog, err := s.nativeTargetFaceResultCatalog(ctx, who, plan)
	if err != nil {
		return zero, err
	}
	detector, err := s.readCompletedNativeTargetDetector(ctx, who, plan.detector, color)
	if err != nil {
		return zero, err
	}
	if detector.catalog != plan.prepared.detector {
		return zero, v2input.ErrConflict
	}
	binding := plan.detector.anchor.entry.Binding
	objects, err := s.facePreparationObjects(ctx, who, plan.shell.Tenant, binding, catalog.Outputs[0], "face.landmarks")
	if err != nil {
		return zero, err
	}
	if objects[1].ref != catalog.Params {
		return zero, v2input.ErrConflict
	}
	raw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, objects[0].object, objects[0].ref)
	if err != nil {
		return zero, err
	}
	params, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, objects[1].object, objects[1].ref)
	if err != nil {
		return zero, err
	}
	image, err := review.ParseWorkingImage([]byte(detector.sourceBody))
	if err != nil {
		return zero, err
	}
	source := image.Source(review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256})
	detection := review.ArtifactRef{ID: detector.catalog.Output.ArtifactID, SHA256: detector.catalog.Output.SHA256}
	if err := review.VerifyLandmarkDetection(raw, detection, []byte(detector.body), source); err != nil {
		return zero, err
	}
	if err := review.VerifyLandmarkParams(review.ArtifactRef{ID: catalog.Params.ArtifactID, SHA256: catalog.Params.SHA256}, params, raw, source); err != nil {
		return zero, err
	}
	faces, err := review.ParseFaceLandmarkCollection(raw, source)
	if err != nil {
		return zero, err
	}
	var claims struct {
		Backend   string `json:"backend"`
		ModelLock string `json:"model_lock_id"`
	}
	var expected v2qualification.Expected
	if json.Unmarshal(raw, &claims) != nil || json.Unmarshal([]byte(plan.prepared.expected), &expected) != nil || claims.Backend != "cuda" || claims.ModelLock != expected.ModelLockSHA {
		return zero, v2qualification.ErrUnqualified
	}
	selected, err := selectQualificationPolicies(plan.detector.detector.policy, []string{"face.landmarks"})
	if err != nil {
		return zero, err
	}
	if !sameQualificationPolicyExpected(expected, selected["face.landmarks"]) {
		return zero, v2qualification.ErrUnqualified
	}
	current, err := s.nativeTargetFaceResultCatalog(ctx, who, plan)
	if err != nil {
		return zero, err
	}
	originalSHA, err := canonical.Hash(catalog)
	if err != nil {
		return zero, err
	}
	currentSHA, err := canonical.Hash(current)
	if err != nil || currentSHA != originalSHA {
		return zero, v2input.ErrConflict
	}
	pinned, err := s.facePreparationObjects(ctx, who, plan.shell.Tenant, binding, catalog.Outputs[0], "face.landmarks")
	if err != nil {
		return zero, err
	}
	if pinned != objects {
		return zero, v2input.ErrConflict
	}
	after, err := s.readCompletedNativeTargetDetector(ctx, who, plan.detector, color)
	if err != nil {
		return zero, err
	}
	if after.catalog != detector.catalog || after.body != detector.body || after.sourceBody != detector.sourceBody || after.detectorParamsBody != detector.detectorParamsBody {
		return zero, v2input.ErrConflict
	}
	report, err := s.faceQualificationObject(ctx, who, plan.shell.Tenant, expected, plan.prepared.report.ref)
	if err != nil {
		return zero, err
	}
	if report != plan.prepared.report {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return completedNativeLandmarks{catalog: catalog, body: string(raw), paramsBody: string(params), faces: faces, objects: objects, detector: detector}, nil
}
