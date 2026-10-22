package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type nativeFaceDraftPlan struct {
	seal, key string
	detector  detectorDraftPlan
	prepared  nativeFaceTaskPreparation
	shell     v2input.DraftShell
}

// Only owned blocked metadata is compiled here. Current SQL authority, detector
// dependency locks, registration, quota and admission remain separate gates.
func (s SQLTransferSourceReader) compileNativeFaceDraftPlan(ctx context.Context, who auth.Identity, detector detectorDraftPlan, prepared nativeFaceTaskPreparation, reserved int64) (nativeFaceDraftPlan, error) {
	var zero nativeFaceDraftPlan
	if err := verifyDetectorPlanSeal(ctx, detector); err != nil {
		return zero, err
	}
	owned, err := s.recompileDetectorDraftPlan(ctx, who, detector)
	if err != nil {
		return zero, err
	}
	if owned.seal != detector.seal {
		return zero, v2input.ErrConflict
	}
	if prepared.tenant != owned.request.TenantID || prepared.project != s.ProjectID || prepared.actor != who.UserID || prepared.session != who.SessionID || prepared.parent != owned.request.RunID || prepared.bucket != s.Bucket || prepared.frozen == nil {
		return zero, v2input.ErrInvalid
	}
	run, workflow, err := nativeFacePreparationIDs(owned.request.RunID, prepared.operation)
	if err != nil {
		return zero, err
	}
	if prepared.document != uuid.NewSHA1(owned.request.RunID, []byte("semantic."+prepared.operation+".params:reference")) || prepared.detector.ResultID == uuid.Nil || prepared.detector.Params != (v2input.Ref{ArtifactID: owned.shell.Tasks[0].ParamsArtifactID, SHA256: owned.shell.Tasks[0].ParamsSHA}) {
		return zero, v2input.ErrInvalid
	}
	selected, err := selectNativeQualificationPolicies(owned.policy, []string{prepared.operation})
	if err != nil {
		return zero, err
	}
	var expected v2qualification.Expected
	if len(prepared.expected) > 4<<20 || json.Unmarshal([]byte(prepared.expected), &expected) != nil || !sameQualificationPolicyExpected(expected, selected[prepared.operation]) || (expected.Backend != "cuda" && expected.Backend != "mps") {
		return zero, v2qualification.ErrUnqualified
	}
	report := prepared.report
	if report.contract != "QualificationReport" || report.backend != expected.Backend || report.ref.ArtifactID == uuid.Nil || report.object.SHA256 != report.ref.SHA256 || report.object.Key == "" || report.object.VersionID == "" || report.object.VersionID == "null" || report.object.Bytes < 2 || report.object.Bytes > 8<<20 {
		return zero, v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(report.ref.SHA256); err != nil {
		return zero, v2input.ErrInvalid
	}
	task, err := prepared.frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	expectedTask, err := v2input.TaskID(workflow, prepared.operation, 0)
	if err != nil {
		return zero, err
	}
	taskSHA, err := canonical.Hash(task)
	if err != nil || taskSHA != prepared.taskSHA || task.ID != expectedTask || task.Operation != prepared.operation || task.OperationVersion != "1" || task.ResourceClass != "gpu" || task.ParamsArtifactID != prepared.params.ArtifactID || task.ParamsSHA != prepared.params.SHA256 {
		return zero, v2input.ErrConflict
	}
	var evidence []v2draft.InputEvidence
	if len(prepared.inputs) > 4<<20 || json.Unmarshal([]byte(prepared.inputs), &evidence) != nil {
		return zero, v2input.ErrInvalid
	}
	if err := verifyNativeFaceSourceEvidence(ctx, owned.prepared.source, prepared.detector.Output, prepared.params, evidence); err != nil {
		return zero, err
	}
	entries := make([]v2input.Entry, len(evidence))
	for i, pin := range evidence {
		contract := pin.Contract
		switch contract {
		case "WorkingPixels", "WorkingAlpha", "DenseWarpField":
			contract = "NpyArray"
		}
		entries[i] = v2input.Entry{Ref: pin.Ref, Contract: contract}
	}
	switch frozen := prepared.frozen.(type) {
	case v2input.FrozenSemanticLandmarkTask:
		if prepared.operation != "face.landmarks" {
			return zero, v2input.ErrInvalid
		}
		if err := frozen.VerifyInputInventory(ctx, entries); err != nil {
			return zero, err
		}
	case v2input.FrozenSemanticFaceParseTask:
		if prepared.operation != "face.parse" {
			return zero, v2input.ErrInvalid
		}
		if err := frozen.VerifyInputInventory(ctx, entries); err != nil {
			return zero, err
		}
	default:
		return zero, v2input.ErrInvalid
	}
	var profile struct {
		Backend   string `json:"backend"`
		Resources struct {
			Output int64 `json:"output_bytes"`
		} `json:"resource_estimate"`
	}
	if json.Unmarshal(task.ExecutionProfile, &profile) != nil || profile.Backend != expected.Backend || profile.Resources.Output < 1 || reserved < profile.Resources.Output+(8<<20) || reserved > 9007199254740991 {
		return zero, v2input.ErrInvalid
	}
	policySHA, err := canonical.Hash(json.RawMessage(owned.policy.body))
	if err != nil || prepared.policySHA != policySHA {
		return zero, v2input.ErrConflict
	}
	recovery, err := nativePlanRecoveryFromTask(task, &prepared.report.ref, prepared.costs, reserved)
	if err != nil {
		return zero, err
	}
	configValue := map[string]any{"project_id": s.ProjectID, "v27_face_prepare": map[string]any{"recovery": recovery, "schema_version": 1, "operation": prepared.operation, "parent_run_id": owned.request.RunID, "request_sha": owned.request.RequestSHA, "auth_session_id": who.SessionID, "source": owned.request.Intent.Reference, "intent": owned.published.Ref, "snapshot": owned.anchor.snapshotRef, "params_document": prepared.document, "params": prepared.params, "task_id": task.ID, "input_digest": task.InputDigest, "policy_sha": policySHA, "detector_run_id": owned.shell.Run, "detector_task_id": owned.shell.Tasks[0].ID, "detector_result_id": prepared.detector.ResultID, "detector_output": prepared.detector.Output, "detector_params": prepared.detector.Params}}
	if !prepared.legacyQueueOmitted {
		configValue["queue_class"] = "batch"
	}
	config, err := canonical.Marshal(configValue)
	if err != nil {
		return zero, err
	}
	configSHA, err := canonical.Hash(json.RawMessage(config))
	if err != nil {
		return zero, err
	}
	shell := v2input.DraftShell{Tenant: owned.request.TenantID, Run: run, WorkflowID: workflow, Project: s.ProjectID, Snapshot: owned.request.Intent.TargetSnapshotID, Actor: who.UserID, Workflow: "semantic", PolicySHA: policySHA, ConfigSHA: configSHA, Config: config, Tasks: []v2input.DraftTask{task}, Edges: []v2input.DraftEdge{}, Bytes: reserved}
	plan := nativeFaceDraftPlan{key: "face-prep:" + run.String(), detector: owned, prepared: prepared, shell: shell}
	plan.seal, err = nativeFacePlanDigest(plan)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return plan, nil
}

func nativeFacePlanDigest(plan nativeFaceDraftPlan) (string, error) {
	if plan.prepared.frozen == nil {
		return "", v2input.ErrInvalid
	}
	task, err := plan.prepared.frozen.DraftTask()
	if err != nil {
		return "", err
	}
	parent, err := detectorPlanDigest(plan.detector)
	if err != nil {
		return "", err
	}
	p := plan.prepared
	return canonical.Hash(map[string]any{"detector_plan": parent, "key": plan.key, "shell": plan.shell, "qualified_costs": p.costs, "operation": p.operation, "params": p.params, "document": p.document, "detector_result": p.detector, "task": task, "task_sha": p.taskSHA, "input_objects": p.inputs, "expected": p.expected, "report_ref": p.report.ref, "report_object": p.report.object, "report_contract": p.report.contract, "report_backend": p.report.backend, "metrics": qualificationResourceSeal(p.metrics), "scope": map[string]any{"tenant": p.tenant, "project": p.project, "actor": p.actor, "session": p.session, "parent": p.parent, "bucket": p.bucket, "policy_sha": p.policySHA}})
}
func verifyNativeFacePlanSeal(ctx context.Context, plan nativeFaceDraftPlan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if plan.seal == "" {
		return v2input.ErrInvalid
	}
	digest, err := nativeFacePlanDigest(plan)
	if err != nil {
		return err
	}
	if digest != plan.seal {
		return v2input.ErrConflict
	}
	return verifyDetectorPlanSeal(ctx, plan.detector)
}
