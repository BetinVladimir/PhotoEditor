package semanticproposal

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Unique current CPU defect qualification with physical bytes, bounded metadata
// and repeated policy/candidate/object pins. This is a compiler input, never
// defect result authority or public parent admission.
func (s SQLTransferSourceReader) selectInitialDefectConfiguration(ctx context.Context, who auth.Identity, tenantID uuid.UUID, capacity TransferPreparation, reserved int64) (nativeFacePipelineStageConfiguration, error) {
	var zero nativeFacePipelineStageConfiguration
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if err := validateTransferCapacityConfiguration(capacity); err != nil {
		return zero, err
	}
	if _, err := initialNativeDefectBudget(capacity, reserved, v2qualification.Metrics{}); err != nil {
		return zero, err
	}
	snapshot, err := s.readQualificationPolicySnapshot(ctx, who, tenantID)
	if err != nil {
		return zero, err
	}
	selected, err := selectQualificationPolicies(snapshot, []string{"defects.propose"})
	if err != nil {
		return zero, err
	}
	expected := ownQualificationExpected(selected["defects.propose"])
	if expected.Backend != "cpu" {
		return zero, v2qualification.ErrUnqualified
	}
	report, err := s.discoverDefectQualificationReport(ctx, who, tenantID, expected)
	if err != nil {
		return zero, err
	}
	pin, err := s.defectQualificationObject(ctx, who, tenantID, expected, report)
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
	raw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, pin.object, report)
	if err != nil {
		return zero, err
	}
	verified, err := v2qualification.Verify(raw, report.SHA256, expected)
	if err != nil {
		return zero, err
	}
	budget, err := initialNativeDefectBudget(capacity, reserved, verified.Metrics)
	if err != nil {
		return zero, err
	}
	current, err := s.defectQualificationObject(ctx, who, tenantID, expected, report)
	if err != nil {
		return zero, err
	}
	if current != pin {
		return zero, v2input.ErrConflict
	}
	after, err := s.discoverDefectQualificationReport(ctx, who, tenantID, expected)
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

// Native defects are CPU-only. Qualification cannot introduce GPU costs into
// this stage; face producer budgets remain independently qualified.
func initialNativeDefectBudget(capacity TransferPreparation, reserved int64, metrics v2qualification.Metrics) (nativePreparationBudget, error) {
	budget, err := initialNativeFaceBudget(capacity, reserved, metrics)
	if err != nil {
		return nativePreparationBudget{}, err
	}
	if budget.Costs.VRAMBytes != 0 {
		return nativePreparationBudget{}, v2input.ErrInvalid
	}
	budget.VRAM = 0
	return budget, nil
}

// Faces are server-selected locators; bootstrapManualDefect independently
// resolves every original manual raster/vector producer before publication.
// This does not select a bare path when manual authority is absent or invalid.
func (b nativePreparationBootstrap) bootstrapQualifiedManualDefect(ctx context.Context, who auth.Identity, producers nativeInputRestoredFaceProducers, color TransferColorProfile, faces []review.DefectProposalFaceInput, diameter [2]float64, capacity TransferPreparation, reserved int64, documents v2input.DocumentPublisher) (nativeInputManualInitialSubmission, error) {
	var zero nativeInputManualInitialSubmission
	if err := b.validate(ctx); err != nil {
		return zero, err
	}
	if err := verifyNativeInputFaceProducerPlans(ctx, who, producers); err != nil {
		return zero, err
	}
	if documents == nil || capacity.Color.sha != color.sha {
		return zero, v2input.ErrInvalid
	}
	configuration, err := b.Reader.selectInitialDefectConfiguration(ctx, who, producers.detector.request.TenantID, ownTransferPreparation(capacity), reserved)
	if err != nil {
		return zero, err
	}
	run, _, err := defectPreparationIDs(producers.detector.request.RunID)
	if err != nil {
		return zero, err
	}
	b.Transport = v2input.ScopedPublisher{Documents: documents, Tenant: producers.detector.request.TenantID, Run: run}
	return b.bootstrapManualDefect(ctx, who, producers, color, faces, diameter, configuration.Report, configuration.Budget)
}
