package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type personDraftPlan struct {
	seal       string
	requestKey string
	request    Request
	published  PublishedIntent
	anchor     WorkflowPlan
	shell      v2input.DraftShell
	prepared   sourcePersonPreparation
	policy     qualificationPolicySnapshot
	key        string
}

// Builds an owned blocked shell only. SQL admission must separately compose
// parent inventory/snapshot/intent barriers and original source/report locks.
func (s SQLTransferSourceReader) compilePersonDraftPlan(ctx context.Context, who auth.Identity, request Request, key string, published PublishedIntent, snapshot v2input.Ref, snapshotBody []byte, prepared sourcePersonPreparation, policy qualificationPolicySnapshot, reserved int64) (personDraftPlan, error) {
	var zero personDraftPlan
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
	selected, err := selectQualificationPolicies(policy, []string{"mask.person"})
	if err != nil {
		return zero, err
	}
	expected := selected["mask.person"]
	if _, err := s.marshalPersonQualificationProof(ctx, who, owned.TenantID, prepared.qualification, expected); err != nil {
		return zero, err
	}
	if err := verifyPersonStageIdentity(owned.RunID, prepared); err != nil {
		return zero, err
	}
	if err := verifyPersonTaskQualificationBinding(prepared.catalog.frozen, prepared.qualification); err != nil {
		return zero, err
	}
	if err := verifyPersonSourceInputPins(ctx, prepared.catalog, prepared.source); err != nil {
		return zero, err
	}
	if _, err := marshalPersonParamsPin(ctx, s.Bucket, prepared); err != nil {
		return zero, err
	}
	task, err := prepared.catalog.frozen.DraftTask()
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
	recovery, err := nativePlanRecoveryFromTask(task, &prepared.qualification.report.ref, prepared.costs, reserved)
	if err != nil {
		return zero, err
	}
	if prepared.continuation != "" && prepared.continuation != "ordinary_parent" {
		return zero, v2input.ErrInvalid
	}
	if (prepared.continuation == "ordinary_parent") != (prepared.ordinaryPolicy != nil) {
		return zero, v2input.ErrInvalid
	}
	if prepared.ordinaryPolicy != nil {
		if prepared.ordinaryPolicy.ProjectID != s.ProjectID {
			return zero, v2input.ErrConflict
		}
		if _, err := prepared.ordinaryPolicy.preparation(TransferColorProfile{sha: prepared.source.iccSHA}); err != nil {
			return zero, err
		}
	}
	recovery.OrdinaryPolicy = prepared.ordinaryPolicy
	recovery.Continuation = prepared.continuation
	recovery.RequestKey = key
	policySHA, err := canonical.Hash(json.RawMessage(policy.body))
	if err != nil {
		return zero, err
	}
	run, workflow, err := personPreparationIDs(owned.RunID)
	if err != nil {
		return zero, err
	}
	configValue := map[string]any{"project_id": s.ProjectID, "v27_person_prepare": map[string]any{"schema_version": 1, "parent_run_id": owned.RunID, "request_sha": owned.RequestSHA, "auth_session_id": who.SessionID, "source": owned.Intent.Reference, "intent": published.Ref, "snapshot": snapshot, "params_document": prepared.document, "params": v2input.Ref{ArtifactID: task.ParamsArtifactID, SHA256: task.ParamsSHA}, "task_id": task.ID, "input_digest": task.InputDigest, "policy_sha": policySHA, "recovery": recovery}}
 if !prepared.legacyQueueOmitted { configValue["queue_class"]="batch" }
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
	plan := personDraftPlan{requestKey: key, request: owned, published: PublishedIntent{Ref: published.Ref, Inventory: inventory}, anchor: anchor, shell: shell, prepared: prepared, policy: policy, key: "person-prep:" + run.String()}
	plan.seal, err = personPlanDigest(plan)
	if err != nil {
		return zero, err
	}
	return plan, nil
}
