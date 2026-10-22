package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type detectorDraftPlan struct {
	seal       string
	requestKey string
	request    Request
	published  PublishedIntent
	anchor     WorkflowPlan
	shell      v2input.DraftShell
	prepared   sourceDetectorPreparation
	policy     qualificationPolicySnapshot
	key        string
}

// Builds an owned blocked shell only. SQL admission must separately compose
// parent inventory/snapshot/intent barriers and original source/report locks.
func (s SQLTransferSourceReader) compileDetectorDraftPlan(ctx context.Context, who auth.Identity, request Request, key string, published PublishedIntent, snapshot v2input.Ref, snapshotBody []byte, prepared sourceDetectorPreparation, policy qualificationPolicySnapshot, reserved int64) (detectorDraftPlan, error) {
	return s.compileDetectorDraftPlanForQueue(ctx, who, request, key, published, snapshot, snapshotBody, prepared, policy, reserved, true, "batch")
}

// Legacy saved graphs must retain their original config hash during replay.
// New graphs always persist the original parent key in recovery locators.
func (s SQLTransferSourceReader) compileDetectorDraftPlanRecovery(ctx context.Context, who auth.Identity, request Request, key string, published PublishedIntent, snapshot v2input.Ref, snapshotBody []byte, prepared sourceDetectorPreparation, policy qualificationPolicySnapshot, reserved int64, persistKey bool) (detectorDraftPlan, error) {
 return s.compileDetectorDraftPlanForQueue(ctx,who,request,key,published,snapshot,snapshotBody,prepared,policy,reserved,persistKey,"")
}

func (s SQLTransferSourceReader) compileDetectorDraftPlanForQueue(ctx context.Context, who auth.Identity, request Request, key string, published PublishedIntent, snapshot v2input.Ref, snapshotBody []byte, prepared sourceDetectorPreparation, policy qualificationPolicySnapshot, reserved int64, persistKey bool, queue string) (detectorDraftPlan, error) {
 if queue!=""&&queue!="batch" { return detectorDraftPlan{},v2input.ErrInvalid }
	var zero detectorDraftPlan
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	owned, err := CompileRequest(request.TenantID, request.ActorID, key, request.Body)
	if err != nil {
		return zero, err
	}
	if who.UserID != owned.ActorID || who.SessionID == uuid.Nil || owned.RunID != request.RunID || owned.RequestSHA != request.RequestSHA || owned.WorkflowID != request.WorkflowID || prepared.parent != owned.RunID || prepared.source.binding != owned.Intent.Reference || published.Inventory.Scope.ProjectID != s.ProjectID {
		return zero, v2input.ErrInvalid
	}
	intentSHA, err := canonical.Hash(owned.Intent)
	if err != nil || published.Ref.ArtifactID == uuid.Nil || published.Ref.SHA256 != intentSHA {
		return zero, v2input.ErrInvalid
	}
	targets, err := BindTargets(owned, snapshot, snapshotBody)
	if err != nil || len(targets) != len(published.Inventory.Targets) || len(targets) != published.Inventory.Scope.ItemCount {
		return zero, v2input.ErrInvalid
	}
	for i, target := range targets {
		if target != published.Inventory.Targets[i] {
			return zero, v2input.ErrConflict
		}
	}
	if !validQualificationPolicySnapshot(s, who, owned.TenantID, policy) {
		return zero, v2input.ErrInvalid
	}
	selected, err := selectQualificationPolicies(policy, []string{"face.detect"})
	if err != nil {
		return zero, err
	}
	expected := selected["face.detect"]
	if _, err := s.marshalDetectorQualificationProof(ctx, who, owned.TenantID, prepared.qualification, expected); err != nil {
		return zero, err
	}
	if err := verifyDetectorStageIdentity(owned.RunID, prepared); err != nil {
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
	var budget struct {
		Resources struct {
			Output int64 `json:"output_bytes"`
		} `json:"resource_estimate"`
	}
	if json.Unmarshal(task.ExecutionProfile, &budget) != nil || budget.Resources.Output < 1 || reserved < budget.Resources.Output+(8<<20) || reserved > 9007199254740991 {
		return zero, v2input.ErrInvalid
	}
	policySHA, err := canonical.Hash(json.RawMessage(policy.body))
	if err != nil {
		return zero, err
	}
	run, workflow, err := detectorPreparationIDs(owned.RunID)
	if err != nil {
		return zero, err
	}
	recovery, err := nativePlanRecoveryFromTask(task, &prepared.qualification.report.ref, prepared.costs, reserved)
	if err != nil {
		return zero, err
	}
	if persistKey {
		recovery.RequestKey = key
	}
	recovery.ScenePolicy=s.scenePolicy
	configValue := map[string]any{"project_id": s.ProjectID, "v27_detector_prepare": map[string]any{"recovery": recovery, "schema_version": 1, "parent_run_id": owned.RunID, "request_sha": owned.RequestSHA, "auth_session_id": who.SessionID, "source": owned.Intent.Reference, "intent": published.Ref, "snapshot": snapshot, "params_document": prepared.document, "params": v2input.Ref{ArtifactID: task.ParamsArtifactID, SHA256: task.ParamsSHA}, "task_id": task.ID, "input_digest": task.InputDigest, "policy_sha": policySHA}}
 if queue!="" { configValue["queue_class"]=queue }
 config, err := canonical.Marshal(configValue)
	if err != nil {
		return zero, err
	}
	configSHA, err := canonical.Hash(json.RawMessage(config))
	if err != nil {
		return zero, err
	}
	inventory := CatalogInventory{Scope: published.Inventory.Scope, Targets: append([]v2input.SessionVersionBinding(nil), targets...)}
	anchor := WorkflowPlan{tenant: owned.TenantID, actor: owned.ActorID, project: s.ProjectID, run: owned.RunID, requestSHA: owned.RequestSHA, snapshot: owned.Intent.TargetSnapshotID, snapshotRef: snapshot, snapshotBody: append(json.RawMessage(nil), snapshotBody...), inventory: inventory}
	shell := v2input.DraftShell{Tenant: owned.TenantID, Run: run, WorkflowID: workflow, Project: s.ProjectID, Snapshot: owned.Intent.TargetSnapshotID, Actor: owned.ActorID, Workflow: "semantic", PolicySHA: policySHA, ConfigSHA: configSHA, Config: config, Tasks: []v2input.DraftTask{task}, Edges: []v2input.DraftEdge{}, Bytes: reserved}
	plan := detectorDraftPlan{requestKey: key, request: owned, published: PublishedIntent{Ref: published.Ref, Inventory: inventory}, anchor: anchor, shell: shell, prepared: prepared, policy: policy, key: "detector-prep:" + run.String()}
	plan.seal, err = detectorPlanDigest(plan)
	if err != nil {
		return zero, err
	}
	return plan, nil
}

// Recompile in the original metadata format. The complete config still has to
// match the compiler output; a saved key is a locator, never admission authority.
func (s SQLTransferSourceReader) recompileDetectorDraftPlan(ctx context.Context, who auth.Identity, plan detectorDraftPlan) (detectorDraftPlan, error) {
	var zero detectorDraftPlan
	if err := verifyDetectorPlanSeal(ctx, plan); err != nil {
		return zero, err
	}
	var envelope struct {
 QueueClass string `json:"queue_class,omitempty"`
		Stage struct {
			Recovery json.RawMessage `json:"recovery"`
		} `json:"v27_detector_prepare"`
	}
	if len(plan.shell.Tasks) != 1 || json.Unmarshal(plan.shell.Config, &envelope) != nil {
		return zero, v2input.ErrInvalid
	}
	recovery, err := decodeNativePlanRecoveryMetadata(envelope.Stage.Recovery, plan.shell.Tasks[0])
	if err != nil {
		return zero, err
	}
	if recovery.RequestKey != "" && recovery.RequestKey != plan.requestKey {
		return zero, v2input.ErrConflict
	}
	s.scenePolicy=recovery.ScenePolicy
	return s.compileDetectorDraftPlanForQueue(ctx, who, plan.request, plan.requestKey, plan.published, plan.anchor.snapshotRef, plan.anchor.snapshotBody, plan.prepared, plan.policy, plan.shell.Bytes, recovery.RequestKey != "", envelope.QueueClass)
}
