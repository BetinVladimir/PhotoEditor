package semanticproposal

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"reflect"
	"sort"
)

type nativeTargetDefectDraftPlan struct {
	anchor    nativeTargetAnchor
	prepared  nativeTargetDefectTaskPreparation
	shell     v2input.DraftShell
	key, seal string
}

// Pure owned blocked graph compiler. Physical input preparation precedes this;
// SQL dependency locks, blocked begin, registration and seal remain separate.
func (s SQLTransferSourceReader) compileNativeTargetDefectDraftPlan(ctx context.Context, who auth.Identity, p nativeTargetDefectTaskPreparation, reserved int64) (nativeTargetDefectDraftPlan, error) {
	var zero nativeTargetDefectDraftPlan
	if err := s.verifyNativeTargetDefectQualification(ctx, who, p.preparation, p.qualification); err != nil {
		return zero, err
	}
	digest, err := targetDefectTaskDigest(p)
	if err != nil || p.seal == "" || digest != p.seal {
		return zero, v2input.ErrConflict
	}
	task, err := p.frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	recovery, err := nativePlanRecoveryFromTask(task, &p.qualification.report.report.ref, p.qualification.costs, reserved)
	if err != nil {
		return zero, err
	}
	if recovery.VRAM != 0 {
		return zero, v2input.ErrInvalid
	}
	owned, err := s.compileNativeTargetDefectTask(ctx, who, p.preparation, p.qualification, p.params, p.personPixels, recovery.RAM, recovery.Scratch, recovery.Output)
	if err != nil {
		return zero, err
	}
	owned.legacyQueueOmitted = p.legacyQueueOmitted
	rebuilt, err := owned.frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	if owned.seal != p.seal || !reflect.DeepEqual(task, rebuilt) || owned.entries != p.entries || owned.sizes != p.sizes {
		return zero, v2input.ErrConflict
	}
	a := p.preparation.landmarksPlan.detector.anchor
	stage, err := compileNativeTargetAnchor(ctx, who, a.request, a.key, a.published, a.snapshot, a.snapshotBody, a.manifest, a.entry.Ordinal, "defects.propose")
	if err != nil {
		return zero, err
	}
	run := uuid.NewSHA1(stage.entry.Namespace, []byte("semantic.defect.prepare:reference"))
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		return zero, err
	}
	id, err := v2input.TaskID(workflow, "defects.propose", 0)
	if err != nil {
		return zero, err
	}
	if task.ID != id || task.Operation != "defects.propose" || task.ResourceClass != "cpu" || task.ParamsArtifactID != p.params.ref.ArtifactID || task.ParamsSHA != p.params.ref.SHA256 {
		return zero, v2input.ErrConflict
	}
	f := p.preparation
	metadata, err := v2input.ResolveSemanticSourceMetadata(stage.entry.Binding, []byte(f.landmarks.detector.completed.sourceBody))
	if err != nil {
		return zero, err
	}
	direct := []v2input.Ref{p.params.ref, stage.entry.Binding.Source, metadata.Inputs.Pixels}
	sort.Slice(direct, func(i, j int) bool { return bytes.Compare(direct[i].ArtifactID[:], direct[j].ArtifactID[:]) < 0 })
	configValue := map[string]any{"project_id": s.ProjectID, "v27_target_defect_prepare": map[string]any{
		"schema_version": 1, "operation": "defects.propose", "recovery": recovery,
		"parent_run_id": stage.request.RunID, "request_sha": stage.request.RequestSHA, "auth_session_id": who.SessionID,
		"target_ordinal": stage.entry.Ordinal, "target_namespace": stage.entry.Namespace, "target_stage_id": stage.stage.ID, "manifest_sha": stage.manifest.sha,
		"source": stage.entry.Binding, "intent": stage.published.Ref, "snapshot": stage.snapshot,
		"params_document": targetDefectParamsDocument(stage.entry.Namespace), "params": p.params.ref, "task_id": task.ID, "input_digest": task.InputDigest,
		"policy_sha": f.landmarksPlan.shell.PolicySHA, "preparation_sha": f.seal, "qualification_sha": p.qualification.seal,
		"input_entries": json.RawMessage(p.entries), "input_sizes": json.RawMessage(p.sizes), "direct_inputs": direct,
		"diameter_face_fraction": f.diameter,
		"landmarks_run_id":       f.landmarksPlan.shell.Run, "landmarks_task_id": f.landmarksPlan.shell.Tasks[0].ID, "landmarks_result_id": f.landmarks.catalog.ResultID, "landmarks_outputs": f.landmarks.catalog.Outputs, "landmarks_params": f.landmarks.catalog.Params,
		"skin_run_id": f.skinPlan.shell.Run, "skin_task_id": f.skinPlan.shell.Tasks[0].ID, "skin_result_id": f.skin.catalog.ResultID, "skin_outputs": f.skin.catalog.Outputs, "skin_params": f.skin.catalog.Params,
		"person_run_id": f.personPlan.person.shell.Run, "person_task_id": f.personPlan.person.shell.Tasks[0].ID, "person_result_id": f.person.result, "person_outputs": f.person.outputs, "person_root": f.person.mask.Root, "person_pixels": f.person.mask.Pixels,
	}}
	if !p.legacyQueueOmitted { configValue["queue_class"] = "batch" }
	config, err := canonical.Marshal(configValue)
	if err != nil {
		return zero, err
	}
	configSHA, err := canonical.Hash(json.RawMessage(config))
	if err != nil {
		return zero, err
	}
	shell := v2input.DraftShell{Tenant: stage.request.TenantID, Run: run, WorkflowID: workflow, Project: s.ProjectID, Snapshot: stage.request.Intent.TargetSnapshotID, Actor: who.UserID, Workflow: "semantic", PolicySHA: f.landmarksPlan.shell.PolicySHA, ConfigSHA: configSHA, Config: config, Tasks: []v2input.DraftTask{task}, Edges: []v2input.DraftEdge{}, Bytes: reserved}
	plan := nativeTargetDefectDraftPlan{anchor: stage, prepared: owned, shell: shell, key: "target-defect-prep:" + run.String()}
	plan.seal, err = targetDefectPlanDigest(plan)
	if err != nil {
		return zero, err
	}
	return plan, ctx.Err()
}

func targetDefectPlanDigest(p nativeTargetDefectDraftPlan) (string, error) {
	if err := verifyNativeTargetAnchor(context.Background(), p.anchor); err != nil {
		return "", err
	}
	policy := p.prepared.preparation.landmarksPlan.detector.detector.policy
	scope := SQLTransferSourceReader{ProjectID: policy.project, Bucket: p.prepared.preparation.landmarksPlan.detector.detector.prepared.source.bucket}
	who := auth.Identity{UserID: policy.actor, SessionID: policy.session}
	qualified, err := scope.targetDefectQualificationDigest(context.Background(), who, p.prepared.preparation, p.prepared.qualification)
	if err != nil || qualified != p.prepared.qualification.seal {
		return "", v2input.ErrConflict
	}
	preparation, err := nativeTargetDefectPreparationDigest(p.prepared.preparation)
	if err != nil || preparation != p.prepared.preparation.seal {
		return "", v2input.ErrConflict
	}
	task, err := targetDefectTaskDigest(p.prepared)
	if err != nil || task != p.prepared.seal {
		return "", v2input.ErrConflict
	}
	return canonical.Hash(map[string]any{"anchor": p.anchor.seal, "task": task, "preparation": preparation, "qualification": p.prepared.qualification.seal, "key": p.key, "shell": p.shell})
}

func verifyNativeTargetDefectPlanSeal(ctx context.Context, p nativeTargetDefectDraftPlan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	digest, err := targetDefectPlanDigest(p)
	if err != nil {
		return err
	}
	if p.seal == "" || digest != p.seal {
		return v2input.ErrConflict
	}
	return verifyNativeTargetDefectPreparation(ctx, p.prepared.preparation)
}
