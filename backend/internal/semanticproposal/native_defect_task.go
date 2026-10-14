package semanticproposal

import (
	"context"
	"reflect"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type nativeDefectTaskPreparation struct {
	legacyQueueOmitted bool
	frozen         v2input.FrozenSemanticDefectTask
	dependencies   nativeDefectDependencies
	qualification  nativeDefectStageQualification
	params         defectEvidenceObject
	entries, sizes string
	additional     []nativeDefectInputPin
}

// Pure task compilation: pins are selected/verified by preceding readers; SQL
// locks must repeat exact pins before registration/admission. No IO or queue.
func (s SQLTransferSourceReader) compileNativeDefectTask(ctx context.Context, who auth.Identity, dependencies nativeDefectDependencies, qualification nativeDefectStageQualification, params defectEvidenceObject, additional []nativeDefectInputPin, ram, vram, scratch, output int64) (nativeDefectTaskPreparation, error) {
	var zero nativeDefectTaskPreparation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	digest, err := nativeDefectDependencyDigest(dependencies)
	if err != nil {
		return zero, err
	}
	if dependencies.seal == "" || digest != dependencies.seal {
		return zero, v2input.ErrConflict
	}
	prepared := dependencies.preparation
	landmarks, err := s.compileNativeFaceDraftPlan(ctx, who, prepared.landmarksPlan.detector, prepared.landmarksPlan.prepared, prepared.landmarksPlan.shell.Bytes)
	if err != nil {
		return zero, err
	}
	skin, err := s.compileNativeSkinDraftPlan(ctx, who, prepared.skinPlan.parser, prepared.skinPlan.prepared, prepared.skinPlan.shell.Bytes)
	if err != nil {
		return zero, err
	}
	if landmarks.seal != prepared.landmarksPlan.seal || skin.seal != prepared.skinPlan.seal || qualification.landmarksSeal != landmarks.seal || qualification.skinSeal != skin.seal || !reflect.DeepEqual(qualification.policy, landmarks.detector.policy) {
		return zero, v2input.ErrConflict
	}
	tenant := landmarks.shell.Tenant
	if !validQualificationPolicySnapshot(s, who, tenant, qualification.policy) {
		return zero, v2input.ErrInvalid
	}
	selected, err := selectQualificationPolicies(qualification.policy, []string{"defects.propose"})
	if err != nil {
		return zero, err
	}
	expected := selected["defects.propose"]
	if expected.Backend != "cpu" {
		return zero, v2qualification.ErrUnqualified
	}
	if _, err := s.marshalDefectQualificationPreparation(ctx, who, tenant, qualification.report, expected); err != nil {
		return zero, err
	}
	if err := validateTransferMeasuredCosts(qualification.costs, qualification.report.metrics); err != nil {
		return zero, err
	}
	entries, sizes, err := compileNativeDefectInventory(ctx, prepared, params, additional)
	if err != nil {
		return zero, err
	}
	run := uuid.NewSHA1(landmarks.detector.request.RunID, []byte("semantic.defect.prepare:reference"))
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		return zero, err
	}
	frozen, err := v2input.CompileSemanticDefectTask(ctx, workflow, 0, landmarks.detector.request.Intent.Reference, []byte(prepared.landmarks.detector.sourceBody), []byte(prepared.params.params), params.ref, entries, sizes, qualification.costs, ram, vram, scratch, output)
	if err != nil {
		return zero, err
	}
	entryBytes, err := canonical.Marshal(entries)
	if err != nil {
		return zero, err
	}
	sizeBytes, err := canonical.Marshal(sizes)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return nativeDefectTaskPreparation{frozen: frozen, dependencies: dependencies, qualification: qualification, params: params, entries: string(entryBytes), sizes: string(sizeBytes), additional: append([]nativeDefectInputPin(nil), additional...)}, nil
}
