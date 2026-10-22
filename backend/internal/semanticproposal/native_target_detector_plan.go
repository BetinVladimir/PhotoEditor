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

type nativeTargetDetectorPlan struct {
	anchor   nativeTargetAnchor
	detector detectorDraftPlan // retained task/source proofs; not a reference-stage plan
	seal     string
}

func (s SQLTransferSourceReader) compileNativeTargetDetectorPlan(ctx context.Context, who auth.Identity, a nativeTargetAnchor, prepared sourceDetectorPreparation, policy qualificationPolicySnapshot, reserved int64) (nativeTargetDetectorPlan, error) {
	var zero nativeTargetDetectorPlan
	if err := verifyNativeTargetAnchor(ctx, a); err != nil {
		return zero, err
	}
	owned, err := compileNativeTargetAnchor(ctx, who, a.request, a.key, a.published, a.snapshot, a.snapshotBody, a.manifest, a.entry.Ordinal, "face.detect")
	if err != nil {
		return zero, err
	}
	if owned.seal != a.seal || a.stage.Operation != "face.detect" || prepared.parent != a.entry.Namespace || prepared.source.binding != a.entry.Binding {
		return zero, v2input.ErrConflict
	}
	source := prepared.source
	if source.tenant != a.request.TenantID || source.project != s.ProjectID || source.actor != who.UserID || source.session != who.SessionID || source.bucket != s.Bucket || s.Bucket == "" || !source.unitRGB || a.published.Inventory.Scope.ProjectID != s.ProjectID {
		return zero, v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(source.iccSHA); err != nil {
		return zero, v2input.ErrInvalid
	}
	if prepared.document != uuid.NewSHA1(a.entry.Namespace, []byte("semantic.face.detect.params:reference")) {
		return zero, v2input.ErrConflict
	}
	if !validQualificationPolicySnapshot(s, who, a.request.TenantID, policy) {
		return zero, v2input.ErrInvalid
	}
	selected, err := selectNativeQualificationPolicies(policy, []string{"face.detect"})
	if err != nil {
		return zero, err
	}
	if _, err := s.marshalDetectorQualificationProof(ctx, who, a.request.TenantID, prepared.qualification, selected["face.detect"]); err != nil {
		return zero, err
	}
	if err := verifyDetectorStageIdentity(a.entry.Namespace, prepared); err != nil {
		return zero, err
	}
	if err := verifyDetectorTaskQualificationBinding(prepared.frozen, prepared.qualification); err != nil {
		return zero, err
	}
	if err := verifyDetectorSourceInputPins(ctx, prepared); err != nil {
		return zero, err
	}
	if _, err := marshalDetectorParamsPin(ctx, s.Bucket, prepared); err != nil {
		return zero, err
	}
	task, err := prepared.frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	// New target tasks require explicit model/runtime/device identity; legacy
	// identity-free reference tasks remain governed by their original contracts.
	var profile struct {
		Model     string `json:"model_lock_sha"`
		Runtime   string `json:"runtime_lock_sha"`
		Device    string `json:"device_profile_sha"`
		Resources struct {
			Output int64 `json:"output_bytes"`
		} `json:"resource_estimate"`
	}
	if json.Unmarshal(task.ExecutionProfile, &profile) != nil || profile.Model != selected["face.detect"].ModelLockSHA || profile.Runtime != selected["face.detect"].RuntimeLockSHA || profile.Device != selected["face.detect"].DeviceProfileSHA || profile.Resources.Output < 1 || reserved < profile.Resources.Output+(8<<20) || reserved > 9007199254740991 {
		return zero, v2input.ErrInvalid
	}
	recovery, err := nativePlanRecoveryFromTask(task, &prepared.qualification.report.ref, prepared.costs, reserved)
	if err != nil {
		return zero, err
	}
	recovery.RequestKey = a.key
	policySHA, err := canonical.Hash(json.RawMessage(policy.body))
	if err != nil {
		return zero, err
	}
	run, workflow, err := detectorPreparationIDs(a.entry.Namespace)
	if err != nil {
		return zero, err
	}
	configValue := map[string]any{"project_id": s.ProjectID, "v27_target_detector_prepare": map[string]any{"schema_version": 1, "parent_run_id": a.request.RunID, "request_sha": a.request.RequestSHA, "auth_session_id": who.SessionID, "target_ordinal": a.entry.Ordinal, "target_namespace": a.entry.Namespace, "target_stage_id": a.stage.ID, "manifest_sha": a.manifest.sha, "source": a.entry.Binding, "intent": a.published.Ref, "snapshot": a.snapshot, "params_document": prepared.document, "params": v2input.Ref{ArtifactID: task.ParamsArtifactID, SHA256: task.ParamsSHA}, "task_id": task.ID, "input_digest": task.InputDigest, "policy_sha": policySHA, "recovery": recovery}}
	if !prepared.legacyTargetQueueOmitted {
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
	anchor := WorkflowPlan{tenant: a.request.TenantID, actor: who.UserID, project: s.ProjectID, run: a.request.RunID, requestSHA: a.request.RequestSHA, snapshot: a.request.Intent.TargetSnapshotID, snapshotRef: a.snapshot, snapshotBody: append(json.RawMessage(nil), a.snapshotBody...), inventory: a.published.Inventory}
	shell := v2input.DraftShell{Tenant: a.request.TenantID, Run: run, WorkflowID: workflow, Project: s.ProjectID, Snapshot: a.request.Intent.TargetSnapshotID, Actor: who.UserID, Workflow: "semantic", PolicySHA: policySHA, ConfigSHA: configSHA, Config: config, Tasks: []v2input.DraftTask{task}, Edges: []v2input.DraftEdge{}, Bytes: reserved}
	base := detectorDraftPlan{request: a.request, requestKey: a.key, published: a.published, anchor: anchor, shell: shell, prepared: prepared, policy: policy, key: "target-detector-prep:" + run.String()}
	plan := nativeTargetDetectorPlan{anchor: owned, detector: base}
	plan.seal, err = targetDetectorPlanDigest(plan)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return plan, nil
}
func targetDetectorPlanDigest(p nativeTargetDetectorPlan) (string, error) {
	digest, err := detectorPlanDigest(p.detector)
	if err != nil {
		return "", err
	}
	return canonical.Hash(map[string]any{"target_anchor": p.anchor.seal, "detector_plan": digest})
}
func verifyNativeTargetDetectorPlan(ctx context.Context, p nativeTargetDetectorPlan) error {
	if err := verifyNativeTargetAnchor(ctx, p.anchor); err != nil {
		return err
	}
	digest, err := targetDetectorPlanDigest(p)
	if err != nil {
		return err
	}
	if p.seal == "" || p.seal != digest {
		return v2input.ErrConflict
	}
	return nil
}

// Physical target bytes and trusted qualification use the existing proven lower
// pipeline. Namespace affects IDs only; original proposal remains unchanged.
func (s SQLTransferSourceReader) prepareNativeTargetDetectorPlan(ctx context.Context, catalog v2draft.ArtifactCatalog, publisher v2draft.Publisher, who auth.Identity, a nativeTargetAnchor, color TransferColorProfile, report v2input.Ref, score float64, maxFaces int, budget nativePreparationBudget) (nativeTargetDetectorPlan, error) {
	var zero nativeTargetDetectorPlan
	if err := verifyNativeTargetAnchor(ctx, a); err != nil {
		return zero, err
	}
	if catalog == nil || a.stage.Operation != "face.detect" || budget.OrdinaryPolicy != nil {
		return zero, v2input.ErrInvalid
	}
	policy, err := s.readQualificationPolicySnapshot(ctx, who, a.request.TenantID)
	if err != nil {
		return zero, err
	}
	selected, err := selectNativeQualificationPolicies(policy, []string{"face.detect"})
	if err != nil {
		return zero, err
	}
	expected := selected["face.detect"]
	if expected.Backend != "cuda" && expected.Backend != "mps" {
		return zero, v2qualification.ErrUnqualified
	}
	document := uuid.NewSHA1(a.entry.Namespace, []byte("semantic.face.detect.params:reference"))
	params, err := s.publishSourceDetectorVersionParams(ctx, who, a.request.TenantID, a.entry.Binding, color, publisher, document, expected.ModelLockSHA, score, maxFaces)
	if err != nil {
		return zero, err
	}
	prepared, err := s.prepareSourceDetectorTask(ctx, catalog, who, a.request.TenantID, a.entry.Namespace, 0, a.entry.Binding, color, document, params, report, expected, score, maxFaces, budget.Costs, budget.RAM, budget.VRAM, budget.Scratch, budget.Output)
	if err != nil {
		return zero, err
	}
	if err := s.recheckNativePlanPolicy(ctx, who, a.request.TenantID, policy); err != nil {
		return zero, err
	}
	return s.compileNativeTargetDetectorPlan(ctx, who, a, prepared, policy, budget.Reserved)
}
