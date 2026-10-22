package semanticproposal

import (
	"context"
	"math"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Unique current detector qualification with physical bytes, bounded metadata
// and repeated policy/candidate/object pins. This is a compiler input, never
// detector result authority or public parent admission.
func (s SQLTransferSourceReader) selectInitialDetectorConfiguration(ctx context.Context, who auth.Identity, tenantID uuid.UUID, capacity TransferPreparation, reserved int64) (nativeFacePipelineStageConfiguration, error) {
	var zero nativeFacePipelineStageConfiguration
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if err := validateTransferCapacityConfiguration(capacity); err != nil {
		return zero, err
	}
	if _, err := initialNativeFaceBudget(capacity, reserved, v2qualification.Metrics{}); err != nil {
		return zero, err
	}
	snapshot, err := s.readQualificationPolicySnapshot(ctx, who, tenantID)
	if err != nil {
		return zero, err
	}
	selected, err := selectNativeQualificationPolicies(snapshot, []string{"face.detect"})
	if err != nil {
		return zero, err
	}
	expected := ownQualificationExpected(selected["face.detect"])
	if expected.Backend != "cuda" && expected.Backend != "mps" {
		return zero, v2qualification.ErrUnqualified
	}
	report, err := s.discoverFaceQualificationReport(ctx, who, tenantID, expected)
	if err != nil {
		return zero, err
	}
	pin, err := s.faceQualificationObject(ctx, who, tenantID, expected, report)
	if err != nil {
		return zero, err
	}
	if pin.object.Bytes < 2 || pin.object.Bytes > 8<<20 {
		return zero, v2input.ErrInvalid
	}
	var used int64
	if err := chargeMetadata(&used, capacity.MetadataBytes, len(snapshot.body), int(pin.object.Bytes)); err != nil {
		return zero, err
	}
	verified, err := s.VerifyFaceQualificationTyped(ctx, who, tenantID, report, expected)
	if err != nil {
		return zero, err
	}
	budget, err := initialNativeFaceResourceBudget(capacity, reserved, verified.Resources())
	if err != nil {
		return zero, err
	}
	current, err := s.faceQualificationObject(ctx, who, tenantID, expected, report)
	if err != nil {
		return zero, err
	}
	if current != pin {
		return zero, v2input.ErrConflict
	}
	after, err := s.discoverFaceQualificationReport(ctx, who, tenantID, expected)
	if err != nil {
		return zero, err
	}
	if after != report {
		return zero, v2input.ErrConflict
	}
	if err := s.recheckNativePlanPolicy(ctx, who, tenantID, snapshot); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return nativeFacePipelineStageConfiguration{Report: report, Budget: budget}, nil
}

func validateInitialDetectorControls(score float64, maxFaces int) error {
	if math.IsNaN(score) || math.IsInf(score, 0) || score < .5 || score > .99 || maxFaces < 1 || maxFaces > 100 {
		return v2input.ErrInvalid
	}
	return nil
}

// Server-owned qualified detector start with deterministic transport scope and
// atomic initial scheduling activation. The caller holds the original parent
// lock. Existing detector checkpoints must restore/resume original compiler
// inputs; this new-start boundary is not a fallback for failed restoration.
func (b nativePreparationBootstrap) bootstrapQualifiedInitialDetector(ctx context.Context, who auth.Identity, anchor nativePreparationAnchor, capacity TransferPreparation, reserved int64, documents v2input.DocumentPublisher, score float64, maxFaces int) (detectorDraftPlan, v2input.JobTicket, error) {
	return b.bootstrapQualifiedInitialDetectorMode(ctx, who, anchor, capacity, reserved, documents, score, maxFaces, nil)
}

// Manual selections use the same server qualification as ordinary initial
// detection and are persisted atomically with detector admission.
func (b nativePreparationBootstrap) bootstrapQualifiedInitialManualDetector(ctx context.Context, who auth.Identity, anchor nativePreparationAnchor, capacity TransferPreparation, reserved int64, documents v2input.DocumentPublisher, score float64, maxFaces int, pixels []v2input.Ref, diameter [2]float64) (detectorDraftPlan, v2input.JobTicket, error) {
	if err := ctx.Err(); err != nil {
		return detectorDraftPlan{}, v2input.JobTicket{}, err
	}
	if err := validateInitialManualPixelSelection(pixels); err != nil {
		return detectorDraftPlan{}, v2input.JobTicket{}, err
	}
	if err := validateInitialManualDiameter(diameter); err != nil {
		return detectorDraftPlan{}, v2input.JobTicket{}, err
	}
	selection := nativeInitialManualSelection{Pixels: append([]v2input.Ref(nil), pixels...), Diameter: diameter}
	return b.bootstrapQualifiedInitialDetectorMode(ctx, who, anchor, capacity, reserved, documents, score, maxFaces, &selection)
}

func (b nativePreparationBootstrap) bootstrapQualifiedInitialDetectorMode(ctx context.Context, who auth.Identity, anchor nativePreparationAnchor, capacity TransferPreparation, reserved int64, documents v2input.DocumentPublisher, score float64, maxFaces int, manual *nativeInitialManualSelection) (detectorDraftPlan, v2input.JobTicket, error) {
	if err := b.validate(ctx); err != nil {
		return detectorDraftPlan{}, v2input.JobTicket{}, err
	}
	if err := validateInitialDetectorControls(score, maxFaces); err != nil {
		return detectorDraftPlan{}, v2input.JobTicket{}, err
	}
	if documents == nil || capacity.Color.sha != anchor.Color.sha {
		return detectorDraftPlan{}, v2input.JobTicket{}, v2input.ErrInvalid
	}
	request, err := ownNativePlanRequest(ctx, who, anchor.Request, anchor.Key)
	if err != nil {
		return detectorDraftPlan{}, v2input.JobTicket{}, err
	}
	configuration, err := b.Reader.selectInitialDetectorConfiguration(ctx, who, request.TenantID, ownTransferPreparation(capacity), reserved)
	if err != nil {
		return detectorDraftPlan{}, v2input.JobTicket{}, err
	}
	run, _, err := detectorPreparationIDs(request.RunID)
	if err != nil {
		return detectorDraftPlan{}, v2input.JobTicket{}, err
	}
	b.Transport = v2input.ScopedPublisher{Documents: documents, Tenant: request.TenantID, Run: run}
	if manual != nil {
		return b.bootstrapInitialDetectorWithManualSelection(ctx, who, anchor, configuration.Report, score, maxFaces, configuration.Budget, manual.Pixels, manual.Diameter)
	}
	return b.bootstrapInitialDetector(ctx, who, anchor, configuration.Report, score, maxFaces, configuration.Budget)
}
