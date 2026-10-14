package semanticproposal

import (
	"context"
	"encoding/json"
	"reflect"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type nativeInputManualInitialDraftPlan struct {
	seal, key string
	prepared  nativeInputManualInitialTaskPreparation
	shell     v2input.DraftShell
}

// Recompiles independently qualified initial inputs before canonical draft
// construction. The graph remains blocked until separate registration/admission.
func (s SQLTransferSourceReader) compileNativeInputManualInitialDraft(ctx context.Context, who auth.Identity, p nativeInputManualInitialTaskPreparation, reserved int64) (nativeInputManualInitialDraftPlan, error) {
	var zero nativeInputManualInitialDraftPlan
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	task, err := p.compiled.frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	var resources struct {
		Backend  string `json:"backend"`
		Estimate struct {
			RAM     int64 `json:"ram_bytes"`
			VRAM    int64 `json:"vram_bytes"`
			Scratch int64 `json:"scratch_bytes"`
			Output  int64 `json:"output_bytes"`
		} `json:"resource_estimate"`
	}
	if json.Unmarshal(task.ExecutionProfile, &resources) != nil || resources.Backend != "cpu" {
		return zero, v2input.ErrInvalid
	}
	owned, err := s.compileNativeInputManualInitialTask(ctx, who, p.stage, resources.Estimate.RAM, resources.Estimate.VRAM, resources.Estimate.Scratch, resources.Estimate.Output)
	if err != nil {
		return zero, err
	}
	owned.legacyQueueOmitted = p.legacyQueueOmitted
	current, err := owned.compiled.frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(task, current) || owned.compiled.entries != p.compiled.entries || owned.compiled.sizes != p.compiled.sizes {
		return zero, v2input.ErrConflict
	}
	deps := owned.stage.dependencies.dependencies
	d, selection := deps.producers, deps.selection
	// Pure canonical assembly data, never a bare native authority certificate.
	faceData := nativeDefectFacePreparation{landmarksPlan: d.landmarks, skinPlan: d.skin, landmarks: selection.landmarks, skin: selection.skin, params: selection.params}
	dependencyData := nativeDefectDependencies{preparation: faceData, landmarks: deps.landmarks, skin: deps.skin}
	dependencyData.seal, err = nativeDefectDependencyDigest(dependencyData)
	if err != nil {
		return zero, err
	}
	data := nativeDefectTaskPreparation{legacyQueueOmitted: owned.legacyQueueOmitted,dependencies: dependencyData, qualification: owned.stage.qualification.qualification, params: owned.stage.dependencies.params.params, entries: owned.compiled.entries, sizes: owned.compiled.sizes}
	shell, run, err := s.assembleNativeDefectDraftShell(ctx, who, data, current, reserved)
	if err != nil {
		return zero, err
	}
	plan := nativeInputManualInitialDraftPlan{key: "defect-prep:" + run.String(), prepared: owned, shell: shell}
	plan.seal, err = nativeInputManualInitialDraftDigest(plan)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return plan, nil
}

func nativeInputManualInitialDraftDigest(plan nativeInputManualInitialDraftPlan) (string, error) {
	stage := plan.prepared.stage
	q := stage.qualification.qualification
	params := stage.dependencies.params.params
	return canonical.Hash(map[string]any{"shell": plan.shell, "key": plan.key, "entries": plan.prepared.compiled.entries, "sizes": plan.prepared.compiled.sizes, "dependency_seal": stage.dependencies.dependencies.seal, "qualified_dependency_seal": stage.qualification.dependencySeal, "qualification_expected": q.report.expected, "qualification_ref": q.report.report.ref, "qualification_object": q.report.report.object, "qualification_backend": q.report.report.backend, "qualification_contract": q.report.report.contract, "qualification_metrics": q.report.metrics, "policy": q.policy.body, "costs": q.costs, "params_ref": params.ref, "params_object": params.object})
}

func verifyNativeInputManualInitialDraftSeal(ctx context.Context, plan nativeInputManualInitialDraftPlan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if plan.seal == "" {
		return v2input.ErrInvalid
	}
	digest, err := nativeInputManualInitialDraftDigest(plan)
	if err != nil {
		return err
	}
	if digest != plan.seal {
		return v2input.ErrConflict
	}
	return nil
}
