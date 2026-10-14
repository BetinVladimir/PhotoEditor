package semanticproposal

import (
	"context"
	"reflect"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type nativeInputManualTaskPreparation struct {
	frozen         v2input.FrozenSemanticDefectTask
	stage          nativeInputRestoredManualStage
	entries, sizes string
}

// Compiles the original restored task using saved resource bounds and costs.
// This distinct proof cannot be passed to the bare native admission path.
func (s SQLTransferSourceReader) compileNativeInputRestoredManualTask(ctx context.Context, who auth.Identity, stage nativeInputRestoredManualStage) (nativeInputManualTaskPreparation, error) {
	var zero nativeInputManualTaskPreparation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	deps, q := stage.dependencies, stage.qualification.qualification
	d := deps.producers.detector
	run, task, _, err := nativeInputSavedStageIdentity(d.request.RunID, "defects.propose")
	if err != nil {
		return zero, err
	}
	m := stage.saved.recovery
	if stage.saved.run != run || stage.saved.task != task || stage.params.ref != stage.saved.params || stage.params.ref != deps.selection.paramsRef || m.Report == nil || q.report.report.ref != *m.Report || !reflect.DeepEqual(q.costs, m.Costs) || stage.qualification.dependencySeal == "" || stage.qualification.dependencySeal != deps.seal {
		return zero, v2input.ErrConflict
	}
	compiled, err := s.compileNativeInputManualTask(ctx, who, deps, stage.qualification, stage.params, m.RAM, m.VRAM, m.Scratch, m.Output)
	if err != nil {
		return zero, err
	}
	return nativeInputManualTaskPreparation{frozen: compiled.frozen, stage: stage, entries: compiled.entries, sizes: compiled.sizes}, nil
}

type nativeInputManualCompiledTask struct {
	frozen         v2input.FrozenSemanticDefectTask
	entries, sizes string
}

// Shared pure compilation after distinct initial/recovery provenance checks.
func (s SQLTransferSourceReader) compileNativeInputManualTask(ctx context.Context, who auth.Identity, deps nativeInputManualDependencies, qualified nativeInputManualStageQualification, params defectEvidenceObject, ram, vram, scratch, output int64) (nativeInputManualCompiledTask, error) {
	var zero nativeInputManualCompiledTask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	q := qualified.qualification
	d := deps.producers.detector
	if qualified.dependencySeal == "" || qualified.dependencySeal != deps.seal || params.ref != deps.selection.paramsRef {
		return zero, v2input.ErrConflict
	}
	run, _, err := defectPreparationIDs(d.request.RunID)
	if err != nil {
		return zero, err
	}
	if q.landmarksSeal != deps.producers.landmarks.seal || q.skinSeal != deps.producers.skin.seal || !validQualificationPolicySnapshot(s, who, d.request.TenantID, q.policy) || !reflect.DeepEqual(q.policy, d.policy) {
		return zero, v2input.ErrConflict
	}
	selected, err := selectQualificationPolicies(q.policy, []string{"defects.propose"})
	if err != nil {
		return zero, err
	}
	expected := selected["defects.propose"]
	if expected.Backend != "cpu" {
		return zero, v2qualification.ErrUnqualified
	}
	if err := validateTransferMeasuredCosts(q.costs, q.report.metrics); err != nil {
		return zero, err
	}
	if _, err := s.marshalDefectQualificationPreparation(ctx, who, d.request.TenantID, q.report, expected); err != nil {
		return zero, err
	}
	entries, sizes, err := s.compileNativeInputManualInventory(ctx, who, deps, params)
	if err != nil {
		return zero, err
	}
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		return zero, err
	}
	frozen, err := v2input.CompileSemanticDefectTask(ctx, workflow, 0, deps.selection.binding, []byte(deps.selection.sourceBody), []byte(deps.selection.params.params), params.ref, entries, sizes, q.costs, ram, vram, scratch, output)
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
	return nativeInputManualCompiledTask{frozen: frozen, entries: string(entryBytes), sizes: string(sizeBytes)}, nil
}
