package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type nativeTargetSkinDraftPlan struct {
	seal, key string
	parser    nativeTargetFaceDraftPlan
	prepared  nativeTargetSkinTaskPreparation
	shell     v2input.DraftShell
}

func (s SQLTransferSourceReader) compileNativeTargetSkinDraftPlan(ctx context.Context, who auth.Identity, parser nativeTargetFaceDraftPlan, p nativeTargetSkinTaskPreparation, reserved int64) (nativeTargetSkinDraftPlan, error) {
	var zero nativeTargetSkinDraftPlan
	if err := verifyNativeTargetFacePlanSeal(ctx, parser); err != nil {
		return zero, err
	}
	owned, err := s.compileNativeTargetFaceDraftPlan(ctx, who, parser.detector, parser.result, parser.prepared, parser.shell.Bytes)
	if err != nil {
		return zero, err
	}
	if owned.seal != parser.seal || parser.prepared.operation != "face.parse" {
		return zero, v2input.ErrConflict
	}
	a := owned.detector.anchor
	if p.tenant != a.request.TenantID || p.project != s.ProjectID || p.actor != who.UserID || p.session != who.SessionID || p.parent != a.entry.Namespace || p.bucket != s.Bucket || p.params.document != uuid.NewSHA1(a.entry.Namespace, []byte("semantic.mask.face_skin.params:reference")) {
		return zero, v2input.ErrInvalid
	}
	c := p.params.parser
	digest, err := targetFaceResultDigest(c)
	if err != nil || digest != c.seal || c.planSeal != owned.seal || c.operation != "face.parse" || c.detector.seal != owned.result.seal || c.catalog.ResultID == uuid.Nil || len(c.catalog.Outputs) != 2 || c.catalog.Params != owned.prepared.params || c.pixels.ref != c.catalog.Outputs[0] || p.paramsObject.ref != p.params.ref {
		return zero, v2input.ErrConflict
	}
	run := uuid.NewSHA1(a.entry.Namespace, []byte("semantic.mask.face_skin:reference"))
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		return zero, err
	}
	task, err := p.frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	id, err := v2input.TaskID(workflow, "mask.face_skin", 0)
	if err != nil {
		return zero, err
	}
	if task.ID != id || task.Operation != "mask.face_skin" || task.ResourceClass != "cpu" || task.ParamsArtifactID != p.params.ref.ArtifactID || task.ParamsSHA != p.params.ref.SHA256 {
		return zero, v2input.ErrConflict
	}
	if err := p.frozen.VerifyInputInventory(ctx, []v2input.Entry{{Ref: p.params.ref, Contract: "Params"}, {Ref: c.catalog.Outputs[1], Contract: "FaceLabels19"}, {Ref: c.pixels.ref, Contract: "NpyArray"}}); err != nil {
		return zero, err
	}
	recovery, err := nativePlanRecoveryFromTask(task, nil, v2input.SemanticQualifiedCosts{}, reserved)
	if err != nil {
		return zero, err
	}
	if reserved < recovery.Output+(8<<20) || reserved > 9007199254740991 {
		return zero, v2input.ErrInvalid
	}
	if c.objects[0].ref != c.catalog.Outputs[1] || c.objects[1].ref != c.catalog.Params || c.objects[0].object.Bytes != int64(len(c.body)) || c.objects[1].object.Bytes != int64(len(c.paramsBody)) || c.pixels.object.SHA256 != c.pixels.ref.SHA256 || p.paramsObject.object.SHA256 != p.params.ref.SHA256 {
		return zero, v2input.ErrConflict
	}
	sizes := []v2input.SemanticInputSize{{Ref: p.params.ref, Bytes: p.paramsObject.object.Bytes}, {Ref: c.catalog.Outputs[1], Bytes: c.objects[0].object.Bytes}, {Ref: c.pixels.ref, Bytes: c.pixels.object.Bytes}}
	d := c.detector.completed
	frozen, err := v2input.CompileSemanticFaceSkinTask(ctx, workflow, 0, a.entry.Binding, json.RawMessage(d.sourceBody), c.catalog.Outputs[1], json.RawMessage(c.body), c.catalog.Params, json.RawMessage(c.paramsBody), d.catalog.Output, json.RawMessage(d.body), p.params.ref, sizes, recovery.RAM, recovery.Scratch, recovery.Output)
	if err != nil {
		return zero, err
	}
	if recovery.SkinProfileVersion == 1 {
		frozen, err = frozen.WithInputIndexScratch()
		if err != nil {
			return zero, err
		}
	}
	rebuilt, err := frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	before, err := canonical.Hash(task)
	if err != nil {
		return zero, err
	}
	after, err := canonical.Hash(rebuilt)
	if err != nil || before != after {
		return zero, v2input.ErrConflict
	}
	p.frozen = frozen
	stage, err := compileNativeTargetAnchor(ctx, who, a.request, a.key, a.published, a.snapshot, a.snapshotBody, a.manifest, a.entry.Ordinal, "mask.face_skin")
	if err != nil {
		return zero, err
	}
	configValue := map[string]any{"project_id": s.ProjectID, "v27_target_skin_prepare": map[string]any{"recovery": recovery, "schema_version": 1, "operation": "mask.face_skin", "target_ordinal": stage.entry.Ordinal, "target_namespace": stage.entry.Namespace, "target_stage_id": stage.stage.ID, "manifest_sha": stage.manifest.sha, "parent_run_id": a.request.RunID, "request_sha": a.request.RequestSHA, "auth_session_id": who.SessionID, "source": a.entry.Binding, "intent": a.published.Ref, "snapshot": a.snapshot, "params_document": p.params.document, "params": p.params.ref, "task_id": task.ID, "input_digest": task.InputDigest, "policy_sha": owned.shell.PolicySHA, "parser_run_id": owned.shell.Run, "parser_task_id": owned.shell.Tasks[0].ID, "parser_result_id": c.catalog.ResultID, "parser_outputs": c.catalog.Outputs, "parser_params": c.catalog.Params}}
	if !p.legacyQueueOmitted { configValue["queue_class"] = "batch" }
	config, err := canonical.Marshal(configValue)
	if err != nil {
		return zero, err
	}
	configSHA, err := canonical.Hash(json.RawMessage(config))
	if err != nil {
		return zero, err
	}
	shell := v2input.DraftShell{Tenant: a.request.TenantID, Run: run, WorkflowID: workflow, Project: s.ProjectID, Snapshot: a.request.Intent.TargetSnapshotID, Actor: who.UserID, Workflow: "semantic", PolicySHA: owned.shell.PolicySHA, ConfigSHA: configSHA, Config: config, Tasks: []v2input.DraftTask{task}, Edges: []v2input.DraftEdge{}, Bytes: reserved}
	plan := nativeTargetSkinDraftPlan{key: "target-skin-prep:" + run.String(), parser: owned, prepared: p, shell: shell}
	plan.seal, err = targetSkinPlanDigest(plan)
	if err != nil {
		return zero, err
	}
	return plan, ctx.Err()
}

func targetSkinPlanDigest(plan nativeTargetSkinDraftPlan) (string, error) {
	parent, err := nativeTargetFacePlanDigest(plan.parser)
	if err != nil {
		return "", err
	}
	result, err := targetFaceResultDigest(plan.prepared.params.parser)
	if err != nil {
		return "", err
	}
	if parent != plan.parser.seal || result != plan.prepared.params.parser.seal {
		return "", v2input.ErrConflict
	}
	task, err := plan.prepared.frozen.DraftTask()
	if err != nil {
		return "", err
	}
	p := plan.prepared
	return canonical.Hash(map[string]any{"parser": parent, "result": result, "key": plan.key, "shell": plan.shell, "task": task, "document": p.params.document, "params": p.params.ref, "params_object": map[string]any{"ref": p.paramsObject.ref, "object": p.paramsObject.object}, "scope": []any{p.tenant, p.project, p.actor, p.session, p.parent, p.bucket}})
}
func verifyNativeTargetSkinPlanSeal(ctx context.Context, plan nativeTargetSkinDraftPlan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if plan.seal == "" {
		return v2input.ErrInvalid
	}
	digest, err := targetSkinPlanDigest(plan)
	if err != nil {
		return err
	}
	if digest != plan.seal {
		return v2input.ErrConflict
	}
	return verifyNativeTargetFacePlanSeal(ctx, plan.parser)
}
