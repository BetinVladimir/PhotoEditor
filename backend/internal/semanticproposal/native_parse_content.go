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

type completedNativeFaceParse struct {
	catalog          nativeFaceResultCatalog
	body, paramsBody string
	labels           review.FaceLabels19
	counts           [19]int64
	objects          [2]defectEvidenceObject
	pixels           defectEvidenceObject
	detector         completedNativeDetector
}

// Verify original immutable BiSeNet manifest/Params and complete uint16 labels before downstream use. This preflight
// does not replace transaction locks or authorize skin/defect admission.
func (s SQLTransferSourceReader) readCompletedNativeFaceParseUncached(ctx context.Context, who auth.Identity, plan nativeFaceDraftPlan, color TransferColorProfile) (completedNativeFaceParse, error) {
	var zero completedNativeFaceParse
	if err := verifyNativeFacePlanSeal(ctx, plan); err != nil {
		return zero, err
	}
	if plan.prepared.operation != "face.parse" {
		return zero, v2input.ErrInvalid
	}
	catalog, err := s.nativeFaceResultCatalog(ctx, who, plan)
	if err != nil {
		return zero, err
	}
	detector, err := s.readCompletedDetectorPreparation(ctx, who, plan.detector, color)
	if err != nil {
		return zero, err
	}
	if detector.catalog != plan.prepared.detector {
		return zero, v2input.ErrConflict
	}
	binding := plan.detector.request.Intent.Reference
	objects, err := s.facePreparationObjects(ctx, who, plan.shell.Tenant, binding, catalog.Outputs[1], "face.parse")
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
	labels, counts, pixels, err := s.readFaceParsePreparationPinned(ctx, who, plan.shell.Tenant, binding, catalog.Outputs[1])
	if err != nil {
		return zero, err
	}
	if labels.Labels != (review.ArtifactRef{ID: catalog.Outputs[0].ArtifactID, SHA256: catalog.Outputs[0].SHA256}) || labels.Faces != (review.ArtifactRef{ID: detector.catalog.Output.ArtifactID, SHA256: detector.catalog.Output.SHA256}) {
		return zero, v2input.ErrConflict
	}
	var expected v2qualification.Expected
	if json.Unmarshal([]byte(plan.prepared.expected), &expected) != nil || labels.Backend != expected.Backend || labels.ModelLockID != expected.ModelLockSHA {
		return zero, v2qualification.ErrUnqualified
	}
	selected, err := selectNativeQualificationPolicies(plan.detector.policy, []string{"face.parse"})
	if err != nil {
		return zero, err
	}
	if !sameQualificationPolicyExpected(expected, selected["face.parse"]) {
		return zero, v2qualification.ErrUnqualified
	}
	current, err := s.nativeFaceResultCatalogCurrent(ctx, who, plan)
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
	pinned, err := s.facePreparationObjects(ctx, who, plan.shell.Tenant, binding, catalog.Outputs[1], "face.parse")
	if err != nil {
		return zero, err
	}
	if pinned != objects {
		return zero, v2input.ErrConflict
	}
	after, err := s.readCompletedDetectorPreparation(ctx, who, plan.detector, color)
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
	return completedNativeFaceParse{catalog: catalog, body: string(raw), paramsBody: string(params), labels: labels, counts: counts, objects: objects, detector: detector, pixels: pixels}, nil
}
