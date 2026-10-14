package semanticproposal

import (
	"context"
	"photoeditor/backend/internal/canonical"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

type nativeInputManualDraftPlan struct {
	seal     string
	prepared nativeInputManualTaskPreparation
	shell    v2input.DraftShell
}

// Rebuilds canonical saved config/profile without granting bare native admission.
func (s SQLTransferSourceReader) compileNativeInputManualDraft(ctx context.Context, who auth.Identity, stage nativeInputRestoredManualStage) (nativeInputManualDraftPlan, error) {
	var zero nativeInputManualDraftPlan
	p, err := s.compileNativeInputRestoredManualTask(ctx, who, stage)
	if err != nil {
		return zero, err
	}
	task, err := p.frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	d, selection := stage.dependencies.producers, stage.dependencies.selection
	// Only canonical assembly data. The independent manual dependency seal and
	// qualification remain in the distinct prepared wrapper returned below.
	faceData := nativeDefectFacePreparation{landmarksPlan: d.landmarks, skinPlan: d.skin, landmarks: selection.landmarks, skin: selection.skin, params: selection.params}
	dependencyData := nativeDefectDependencies{preparation: faceData, landmarks: stage.dependencies.landmarks, skin: stage.dependencies.skin}
	dependencyData.seal, err = nativeDefectDependencyDigest(dependencyData)
	if err != nil {
		return zero, err
	}
	legacyQueue, err := nativePreparationLegacyQueue(stage.saved.config)
	if err != nil { return zero, err }
	data := nativeDefectTaskPreparation{legacyQueueOmitted: legacyQueue,dependencies: dependencyData, qualification: stage.qualification.qualification, params: stage.params, entries: p.entries, sizes: p.sizes}
	shell, _, err := s.assembleNativeDefectDraftShell(ctx, who, data, task, stage.saved.recovery.Reserved)
	if err != nil {
		return zero, err
	}
	if err := matchNativeInputSavedStagePlan(stage.saved, shell); err != nil {
		return zero, err
	}
	plan := nativeInputManualDraftPlan{prepared: p, shell: shell}
	plan.seal, err = nativeInputManualDraftDigest(plan)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return plan, nil
}

func nativeInputManualDraftDigest(plan nativeInputManualDraftPlan) (string, error) {
	stage := plan.prepared.stage
	q := stage.qualification.qualification
	return canonical.Hash(map[string]any{"shell": plan.shell, "entries": plan.prepared.entries, "sizes": plan.prepared.sizes, "dependency_seal": stage.dependencies.seal, "qualified_dependency_seal": stage.qualification.dependencySeal, "qualification_expected": q.report.expected, "qualification_ref": q.report.report.ref, "qualification_object": q.report.report.object, "qualification_backend": q.report.report.backend, "qualification_contract": q.report.report.contract, "qualification_metrics": q.report.metrics, "policy": q.policy.body, "costs": q.costs, "params_ref": stage.params.ref, "params_object": stage.params.object, "saved_config": stage.saved.config, "saved_profile": stage.saved.profile, "saved_recovery": stage.saved.recovery})
}

func verifyNativeInputManualDraftSeal(ctx context.Context, plan nativeInputManualDraftPlan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if plan.seal == "" {
		return v2input.ErrInvalid
	}
	digest, err := nativeInputManualDraftDigest(plan)
	if err != nil {
		return err
	}
	if digest != plan.seal {
		return v2input.ErrConflict
	}
	return nil
}
