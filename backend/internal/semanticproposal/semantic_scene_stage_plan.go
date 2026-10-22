package semanticproposal

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type semanticSceneStagePlan struct {
	prepared  semanticSceneStageTask
	shell     v2input.DraftShell
	key, seal string
}

// The embedded marker is the durable recovery authority, including independent
// qualified report identity. Recovery cannot substitute an unrelated analysis
// with the same image or choose a newly qualified runtime after acceptance.
func (s SQLTransferSourceReader) compileSemanticSceneStagePlan(ctx context.Context, who auth.Identity, request Request, prepared semanticSceneStageTask, reserved int64) (semanticSceneStagePlan, error) {
	var zero semanticSceneStagePlan
	anchor := prepared.published.anchor
	if err := verifySemanticSceneStageAnchor(ctx, who, s.ProjectID, request, anchor); err != nil {
		return zero, err
	}
	task, err := prepared.frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	sha, err := canonical.Hash(task)
	if err != nil || sha != prepared.taskSHA || task.ID != anchor.task || task.Operation != anchor.operation || task.ParamsArtifactID != prepared.published.ref.ArtifactID || task.ParamsSHA != prepared.published.ref.SHA256 {
		return zero, v2input.ErrConflict
	}
	var resources struct {
		Backend  string `json:"backend"`
		Estimate struct {
			Output int64 `json:"output_bytes"`
		} `json:"resource_estimate"`
	}
	if json.Unmarshal(task.ExecutionProfile, &resources) != nil || resources.Estimate.Output < 1 || reserved < resources.Estimate.Output+(8<<20) || reserved > 9007199254740991 {
		return zero, v2input.ErrInvalid
	}
	proof := prepared.published.qualification
	var expected struct {
		Backend string `json:"Backend"`
	}
	if json.Unmarshal([]byte(proof.expected), &expected) != nil || proof.object.backend != expected.Backend || resources.Backend != expected.Backend {
		return zero, v2input.ErrConflict
	}
	if proof.tenant != request.TenantID || proof.project != s.ProjectID || proof.actor != who.UserID || proof.session != who.SessionID || proof.object.contract != "QualificationReport" || (proof.object.backend != "cuda" && proof.object.backend != "mps") {
		return zero, v2input.ErrConflict
	}
	policySHA, err := canonical.Hash(json.RawMessage(proof.expected))
	if err != nil {
		return zero, err
	}
	reportPin := personEvidencePin{proof.object.contract, proof.object.ref.ArtifactID, proof.object.ref.SHA256, s.Bucket, proof.object.object.Key, proof.object.object.VersionID, proof.object.object.Bytes, proof.object.backend}
	config, err := canonical.Marshal(map[string]any{"project_id": s.ProjectID, "queue_class": "batch", "v27_scene_prepare": map[string]any{
		"schema_version": 1, "operation": anchor.operation, "parent_run_id": anchor.parent, "request_sha": anchor.requestSHA, "auth_session_id": anchor.session,
		"source": anchor.source, "source_binding_sha": anchor.bindingSHA, "source_document_sha": anchor.source.Source.SHA256,
		"params_document": anchor.document, "params": prepared.published.ref, "task_id": task.ID, "task_sha": prepared.taskSHA, "input_digest": task.InputDigest, "task": task, "reserved_bytes": reserved,
		"qualification_expected": json.RawMessage(proof.expected), "qualification_report": reportPin, "qualified_costs": prepared.costs, "input_objects": json.RawMessage(prepared.inputs), "source_objects": json.RawMessage(prepared.published.source.pins), "work_icc_sha": prepared.published.source.iccSHA,
	}})
	if err != nil || len(config) > 4<<20 {
		return zero, v2input.ErrInvalid
	}
	configSHA, err := canonical.Hash(json.RawMessage(config))
	if err != nil {
		return zero, err
	}
	shell := v2input.DraftShell{Tenant: request.TenantID, Run: anchor.run, WorkflowID: anchor.workflow, Project: s.ProjectID, Snapshot: request.Intent.TargetSnapshotID, Actor: who.UserID, Workflow: "semantic", PolicySHA: policySHA, ConfigSHA: configSHA, Config: config, Tasks: []v2input.DraftTask{task}, Edges: []v2input.DraftEdge{}, Bytes: reserved}
	key := "scene-prep:" + anchor.run.String()
	seal, err := canonical.Hash(map[string]any{"shell": shell, "key": key, "source_body": anchor.sourceBody, "params_body": prepared.published.body})
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return semanticSceneStagePlan{prepared: prepared, shell: shell, key: key, seal: seal}, nil
}
