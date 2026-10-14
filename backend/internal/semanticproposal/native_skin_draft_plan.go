package semanticproposal

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type nativeSkinDraftPlan struct {
	seal, key string
	parser    nativeFaceDraftPlan
	prepared  nativeSkinTaskPreparation
	shell     v2input.DraftShell
}

// Owns a CPU graph; neither this fingerprint nor its resource profile grants
// registration, qualification or SQL admission authority.
func (s SQLTransferSourceReader) compileNativeSkinDraftPlan(ctx context.Context, who auth.Identity, parser nativeFaceDraftPlan, p nativeSkinTaskPreparation, reserved int64) (nativeSkinDraftPlan, error) {
	var zero nativeSkinDraftPlan
	if err := verifyNativeFacePlanSeal(ctx, parser); err != nil {
		return zero, err
	}
	owned, err := s.compileNativeFaceDraftPlan(ctx, who, parser.detector, parser.prepared, parser.shell.Bytes)
	if err != nil {
		return zero, err
	}
	if owned.seal != parser.seal || parser.prepared.operation != "face.parse" {
		return zero, v2input.ErrConflict
	}
	r := owned.detector.request
	if p.tenant != r.TenantID || p.project != s.ProjectID || p.actor != who.UserID || p.session != who.SessionID || p.parent != r.RunID || p.bucket != s.Bucket || p.params.document != uuid.NewSHA1(r.RunID, []byte("semantic.mask.face_skin.params:reference")) {
		return zero, v2input.ErrInvalid
	}
	c := p.params.parser
	if c.catalog.ResultID == uuid.Nil || len(c.catalog.Outputs) != 2 || c.catalog.Params != owned.prepared.params || c.detector.catalog != owned.prepared.detector || p.labels.ref != c.catalog.Outputs[0] || p.paramsObject.ref != p.params.ref {
		return zero, v2input.ErrConflict
	}
	run := uuid.NewSHA1(r.RunID, []byte("semantic.mask.face_skin:reference"))
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
	if err := p.frozen.VerifyInputInventory(ctx, []v2input.Entry{{Ref: p.params.ref, Contract: "Params"}, {Ref: c.catalog.Outputs[1], Contract: "FaceLabels19"}, {Ref: p.labels.ref, Contract: "NpyArray"}}); err != nil {
		return zero, err
	}
	var profile struct {
		Backend   string `json:"backend"`
		Resources struct {
			Output  int64 `json:"output_bytes"`
			RAM     int64 `json:"ram_bytes"`
			Scratch int64 `json:"scratch_bytes"`
		} `json:"resource_estimate"`
	}
	if json.Unmarshal(task.ExecutionProfile, &profile) != nil || profile.Backend != "cpu" || profile.Resources.Output < 1 || reserved < profile.Resources.Output+(8<<20) || reserved > 9007199254740991 {
		return zero, v2input.ErrInvalid
	}
	// Recompile from retained physical byte counts. A caller must not pair an
	// older frozen profile with changed evidence before obtaining a new seal.
	if c.objects[0].ref != c.catalog.Outputs[1] || c.objects[1].ref != c.catalog.Params || c.objects[0].object.Bytes != int64(len(c.body)) || c.objects[1].object.Bytes != int64(len(c.paramsBody)) || p.labels.object.SHA256 != p.labels.ref.SHA256 || p.paramsObject.object.SHA256 != p.params.ref.SHA256 {
		return zero, v2input.ErrConflict
	}
	sizes := []v2input.SemanticInputSize{{Ref: p.params.ref, Bytes: p.paramsObject.object.Bytes}, {Ref: c.catalog.Outputs[1], Bytes: c.objects[0].object.Bytes}, {Ref: p.labels.ref, Bytes: p.labels.object.Bytes}}
	recovery, err := nativePlanRecoveryFromTask(task, nil, v2input.SemanticQualifiedCosts{}, reserved)
	if err != nil {
		return zero, err
	}
	recompiled, err := v2input.CompileSemanticFaceSkinTask(ctx, workflow, 0, r.Intent.Reference, json.RawMessage(c.detector.sourceBody), c.catalog.Outputs[1], json.RawMessage(c.body), c.catalog.Params, json.RawMessage(c.paramsBody), c.detector.catalog.Output, json.RawMessage(c.detector.body), p.params.ref, sizes, profile.Resources.RAM, profile.Resources.Scratch, profile.Resources.Output)
	if err != nil {
		return zero, err
	}
	if recovery.SkinProfileVersion == 1 {
		recompiled, err = recompiled.WithInputIndexScratch()
		if err != nil {
			return zero, err
		}
	}
	recompiledTask, err := recompiled.DraftTask()
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(task, recompiledTask) {
		return zero, v2input.ErrConflict
	}
	p.frozen = recompiled
	configValue := map[string]any{"project_id": s.ProjectID, "v27_skin_prepare": map[string]any{"recovery": recovery, "schema_version": 1, "operation": "mask.face_skin", "parent_run_id": r.RunID, "request_sha": r.RequestSHA, "auth_session_id": who.SessionID, "source": r.Intent.Reference, "intent": owned.detector.published.Ref, "snapshot": owned.detector.anchor.snapshotRef, "params_document": p.params.document, "params": p.params.ref, "task_id": task.ID, "input_digest": task.InputDigest, "policy_sha": owned.shell.PolicySHA, "parser_run_id": owned.shell.Run, "parser_task_id": owned.shell.Tasks[0].ID, "parser_result_id": c.catalog.ResultID, "parser_outputs": c.catalog.Outputs, "parser_params": c.catalog.Params}}
 if !p.legacyQueueOmitted { configValue["queue_class"]="batch" }
 config, err := canonical.Marshal(configValue)
	if err != nil {
		return zero, err
	}
	configSHA, err := canonical.Hash(json.RawMessage(config))
	if err != nil {
		return zero, err
	}
	shell := v2input.DraftShell{Tenant: r.TenantID, Run: run, WorkflowID: workflow, Project: s.ProjectID, Snapshot: r.Intent.TargetSnapshotID, Actor: who.UserID, Workflow: "semantic", PolicySHA: owned.shell.PolicySHA, ConfigSHA: configSHA, Config: config, Tasks: []v2input.DraftTask{task}, Edges: []v2input.DraftEdge{}, Bytes: reserved}
	plan := nativeSkinDraftPlan{key: "skin-prep:" + run.String(), parser: owned, prepared: p, shell: shell}
	plan.seal, err = nativeSkinPlanDigest(plan)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return plan, nil
}

func nativeSkinPlanDigest(plan nativeSkinDraftPlan) (string, error) {
	parent, err := nativeFacePlanDigest(plan.parser)
	if err != nil {
		return "", err
	}
	p := plan.prepared
	task, err := p.frozen.DraftTask()
	if err != nil {
		return "", err
	}
	c := p.params.parser
	return canonical.Hash(map[string]any{"parser_plan": parent, "key": plan.key, "shell": plan.shell, "task": task, "document": p.params.document, "params": p.params.ref, "params_object": p.paramsObject.object, "labels_ref": p.labels.ref, "labels_object": p.labels.object, "result_id": c.catalog.ResultID, "outputs": c.catalog.Outputs, "parser_params": c.catalog.Params, "manifest": c.body, "parser_body": c.paramsBody, "counts": c.counts, "parsed_labels": c.labels, "parser_objects": []any{c.objects[0].ref, c.objects[0].object, c.objects[1].ref, c.objects[1].object}, "detector_evidence": map[string]any{"catalog": c.detector.catalog, "boxes": c.detector.boxes, "body": c.detector.body, "source": c.detector.sourceBody, "params": c.detector.detectorParamsBody, "objects": []any{c.detector.objects[0].ref, c.detector.objects[0].object, c.detector.objects[1].ref, c.detector.objects[1].object}}, "scope": []any{p.tenant, p.project, p.actor, p.session, p.parent, p.bucket}})
}

func verifyNativeSkinPlanSeal(ctx context.Context, plan nativeSkinDraftPlan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if plan.seal == "" {
		return v2input.ErrInvalid
	}
	digest, err := nativeSkinPlanDigest(plan)
	if err != nil {
		return err
	}
	if digest != plan.seal {
		return v2input.ErrConflict
	}
	return verifyNativeFacePlanSeal(ctx, plan.parser)
}
