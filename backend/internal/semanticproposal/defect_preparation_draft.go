package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type defectPreparationDraftPlan struct {
	request                                              Request
	published                                            PublishedIntent
	anchor                                               WorkflowPlan
	shell                                                v2input.DraftShell
	frozen                                               v2input.FrozenSemanticDefectTask
	execution                                            defectPolicyExecutionPreparation
	params                                               defectVersionParamsProof
	key                                                  string
	requiredProtection, protectionSource, qualifiedFaces string
	originalParams, workingImage                         string
	originSelection                                      defectOriginSelection
}

func defectPreparationIDs(parent uuid.UUID) (uuid.UUID, uuid.UUID, error) {
	if parent == uuid.Nil {
		return uuid.Nil, uuid.Nil, v2input.ErrInvalid
	}
	run := uuid.NewSHA1(parent, []byte("semantic.defect.prepare:reference"))
	workflow, err := v2input.WorkflowID(run)
	return run, workflow, err
}

// Builds a recoverable blocked native preparation stage from privately retained
// evidence. No task is created until Begin executes under caller transaction.
func (s SQLTransferSourceReader) prepareDefectPreparationDraft(ctx context.Context, who auth.Identity, request Request, key string, published PublishedIntent, snapshot v2input.Ref, snapshotBody []byte, execution defectPolicyExecutionPreparation, params defectVersionParamsProof, color TransferColorProfile, costs v2input.SemanticQualifiedCosts, ram, vram, scratch, output, reserved int64) (defectPreparationDraftPlan, error) {
	var zero defectPreparationDraftPlan
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	owned, err := CompileRequest(request.TenantID, request.ActorID, key, request.Body)
	if err != nil {
		return zero, err
	}
	if who.UserID != owned.ActorID || who.SessionID == uuid.Nil || owned.RunID != request.RunID || owned.WorkflowID != request.WorkflowID || owned.RequestSHA != request.RequestSHA || published.Inventory.Scope.ProjectID != s.ProjectID || execution.execution.face.source.source != params.preparation.source || params.preparation.source.binding != owned.Intent.Reference {
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
	if !validQualificationPolicySnapshot(s, who, owned.TenantID, execution.policy) {
		return zero, v2input.ErrInvalid
	}
	if err := matchDefectExecutionPolicySnapshot(execution.execution, execution.policy); err != nil {
		return zero, err
	}
	if err := validateTransferMeasuredCosts(costs, execution.execution.face.source.qualification.metrics); err != nil {
		return zero, err
	}
	files, err := s.decodeDefectExecutionFilePins(ctx, who, owned.TenantID, owned.Intent.Reference, execution.execution.face.source)
	if err != nil {
		return zero, err
	}
	var native []review.ArtifactRef
	if json.Unmarshal([]byte(params.preparation.params.inputs), &native) != nil || len(native) < 2 || len(native) > 4096 {
		return zero, v2input.ErrInvalid
	}
	entries := []v2input.Entry{{Ref: params.params, Contract: "Params"}}
	sizes := []v2input.SemanticInputSize{{Ref: params.params, Bytes: params.object.Bytes}}
	for _, ref := range native {
		found := false
		for _, file := range files {
			if file.Ref.ArtifactID != ref.ID || file.Ref.SHA256 != ref.SHA256 {
				continue
			}
			contract := defectNativeInputContract(file.Contract)
			entries = append(entries, v2input.Entry{Ref: file.Ref, Contract: contract})
			sizes = append(sizes, v2input.SemanticInputSize{Ref: file.Ref, Bytes: file.Object.Bytes})
			found = true
			break
		}
		if !found {
			return zero, v2input.ErrInvalid
		}
	}
	if err := s.recheckManualDefectParams(ctx, who, owned.TenantID, owned.Intent.Reference, color, params.preparation); err != nil {
		return zero, err
	}
	currentParams, err := s.defectVersionParamsObject(ctx, who, owned.TenantID, owned.Intent.Reference, params.document, params.params)
	if err != nil {
		return zero, err
	}
	if currentParams != params.object {
		return zero, v2input.ErrConflict
	}
	run, workflow, err := defectPreparationIDs(owned.RunID)
	if err != nil {
		return zero, err
	}
	image, err := s.Read(ctx, who, owned.TenantID, owned.Intent.Reference)
	if err != nil {
		return zero, err
	}
	if err := s.recheckDefectOriginPreparationProof(ctx, who, owned.TenantID, owned.Intent.Reference, execution.execution.face.originProof); err != nil {
		return zero, err
	}
	originSelection, err := s.selectDefectOriginProducer(ctx, who, owned.TenantID, owned.Intent.Reference, execution.execution.face.originProof.pins)
	if err != nil {
		return zero, err
	}
	originalParams := execution.execution.face.originProof.objects[0]
	originalBody, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, originalParams.object, originalParams.ref)
	if err != nil {
		return zero, err
	}
	parsedImage, err := review.ParseWorkingImage(image)
	if err != nil {
		return zero, err
	}
	protectionSource := parsedImage.Source(review.ArtifactRef{ID: owned.Intent.Reference.Source.ArtifactID, SHA256: owned.Intent.Reference.Source.SHA256})
	originalMasks, err := review.DefectProposalMaskInputs(review.ArtifactRef{ID: originalParams.ref.ArtifactID, SHA256: originalParams.ref.SHA256}, originalBody, protectionSource)
	if err != nil {
		return zero, err
	}
	qualifiedFaces, err := review.DefectProposalFaceInputsFromParams(review.ArtifactRef{ID: originalParams.ref.ArtifactID, SHA256: originalParams.ref.SHA256}, originalBody, protectionSource)
	if err != nil {
		return zero, err
	}
	if err := review.VerifyDefectQualifiedFaceBindings(ctx, review.ArtifactRef{ID: params.params.ArtifactID, SHA256: params.params.SHA256}, []byte(params.preparation.params.params), protectionSource, qualifiedFaces); err != nil {
		return zero, err
	}
	qualifiedFacesJSON, err := canonical.Marshal(qualifiedFaces)
	if err != nil {
		return zero, err
	}
	requiredProtection := make([]review.ProtectionMask, 0)
	for _, mask := range originalMasks {
		if mask.Protected {
			requiredProtection = append(requiredProtection, mask)
		}
	}
	if err := review.VerifyDefectRequiredProtections(ctx, review.ArtifactRef{ID: params.params.ArtifactID, SHA256: params.params.SHA256}, []byte(params.preparation.params.params), protectionSource, requiredProtection); err != nil {
		return zero, err
	}
	if err := s.recheckDefectOriginPreparationProof(ctx, who, owned.TenantID, owned.Intent.Reference, execution.execution.face.originProof); err != nil {
		return zero, err
	}
	requiredJSON, err := canonical.Marshal(requiredProtection)
	if err != nil {
		return zero, err
	}
	protectionSourceJSON, err := canonical.Marshal(protectionSource)
	if err != nil {
		return zero, err
	}
	frozen, err := v2input.CompileSemanticDefectTask(ctx, workflow, 0, owned.Intent.Reference, image, []byte(params.preparation.params.params), params.params, entries, sizes, costs, ram, vram, scratch, output)
	if err != nil {
		return zero, err
	}
	task, err := frozen.DraftTask()
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
	policySHA, err := canonical.Hash(json.RawMessage(execution.policy.body))
	if err != nil {
		return zero, err
	}
	config, err := canonical.Marshal(map[string]any{"project_id": s.ProjectID, "v27_defect_prepare": map[string]any{"schema_version": 1, "parent_run_id": owned.RunID, "request_sha": owned.RequestSHA, "auth_session_id": who.SessionID, "source": owned.Intent.Reference, "intent": published.Ref, "snapshot": snapshot, "params": params.params, "task_id": task.ID, "input_digest": task.InputDigest, "policy_sha": policySHA, "original_producer": originSelection}})
	if err != nil {
		return zero, err
	}
	config, err = s.preserveDefectConfig(ctx, who, owned.TenantID, run, config)
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
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if err := s.recheckManualDefectParams(ctx, who, owned.TenantID, owned.Intent.Reference, color, params.preparation); err != nil {
		return zero, err
	}
	finalParams, err := s.defectVersionParamsObject(ctx, who, owned.TenantID, owned.Intent.Reference, params.document, params.params)
	if err != nil {
		return zero, err
	}
	if finalParams != params.object {
		return zero, v2input.ErrConflict
	}
	finalSelection, err := s.selectDefectOriginProducer(ctx, who, owned.TenantID, owned.Intent.Reference, execution.execution.face.originProof.pins)
	if err != nil {
		return zero, err
	}
	if finalSelection != originSelection {
		return zero, v2input.ErrConflict
	}
	return defectPreparationDraftPlan{request: owned, published: PublishedIntent{Ref: published.Ref, Inventory: inventory}, anchor: anchor, shell: shell, frozen: frozen, execution: execution, params: params, key: "defect-prep:" + run.String(), requiredProtection: string(requiredJSON), protectionSource: string(protectionSourceJSON), qualifiedFaces: string(qualifiedFacesJSON), originalParams: string(originalBody), workingImage: string(image), originSelection: originSelection}, nil
}

// Caller rolls back errors. Creates/resumes only an unsealed draft with a blocked
// task; final input registration and activation are separate operations.
func (s SQLTransferSourceReader) beginDefectPreparationDraftInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, plan defectPreparationDraftPlan) (DraftAdmission, error) {
	var zero DraftAdmission
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if tx == nil || plan.shell.Tenant != plan.request.TenantID || plan.shell.Actor != who.UserID || who.UserID != plan.request.ActorID || who.SessionID != plan.params.preparation.source.session {
		return zero, v2input.ErrInvalid
	}
	stageRun, stageWorkflow, err := defectPreparationIDs(plan.request.RunID)
	if err != nil || plan.shell.Run != stageRun || plan.shell.WorkflowID != stageWorkflow || plan.key != "defect-prep:"+stageRun.String() || plan.shell.Project != s.ProjectID || plan.shell.Snapshot != plan.request.Intent.TargetSnapshotID || len(plan.shell.Tasks) != 1 {
		return zero, v2input.ErrInvalid
	}
	frozenTask, err := plan.frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	frozenJSON, err := canonical.Marshal(frozenTask)
	if err != nil {
		return zero, err
	}
	shellJSON, err := canonical.Marshal(plan.shell.Tasks[0])
	if err != nil || string(frozenJSON) != string(shellJSON) {
		return zero, v2input.ErrInvalid
	}
	if len(plan.requiredProtection) < 2 || len(plan.requiredProtection) > 1048576 || len(plan.protectionSource) < 2 || len(plan.protectionSource) > 1048576 {
		return zero, v2input.ErrInvalid
	}
	var required []review.ProtectionMask
	var source review.DefectSource
	if json.Unmarshal([]byte(plan.requiredProtection), &required) != nil || json.Unmarshal([]byte(plan.protectionSource), &source) != nil || source.Image.ID != plan.request.Intent.Reference.Source.ArtifactID || source.Image.SHA256 != plan.request.Intent.Reference.Source.SHA256 {
		return zero, v2input.ErrInvalid
	}
	if err := review.VerifyDefectRequiredProtections(ctx, review.ArtifactRef{ID: plan.params.params.ArtifactID, SHA256: plan.params.params.SHA256}, []byte(plan.params.preparation.params.params), source, required); err != nil {
		return zero, err
	}
	if len(plan.qualifiedFaces) < 2 || len(plan.qualifiedFaces) > 1048576 {
		return zero, v2input.ErrInvalid
	}
	var qualified []review.DefectProposalFaceInput
	if json.Unmarshal([]byte(plan.qualifiedFaces), &qualified) != nil {
		return zero, v2input.ErrInvalid
	}
	if err := review.VerifyDefectQualifiedFaceBindings(ctx, review.ArtifactRef{ID: plan.params.params.ArtifactID, SHA256: plan.params.params.SHA256}, []byte(plan.params.preparation.params.params), source, qualified); err != nil {
		return zero, err
	}
	content, err := marshalDefectActivationContent(ctx, plan)
	if err != nil {
		return zero, err
	}
	if err := LockInventoryInTransaction(ctx, tx, who, plan.request, plan.published.Inventory); err != nil {
		return zero, err
	}
	if err := LockPublishedIntentInTransaction(ctx, tx, who, plan.request, plan.published); err != nil {
		return zero, err
	}
	if err := LockSnapshotInTransaction(ctx, tx, who, plan.request, plan.anchor); err != nil {
		return zero, err
	}
	if err := s.lockPublishedDefectExecutionInTransaction(ctx, tx, who, plan.request.TenantID, plan.request.Intent.Reference, plan.execution, plan.params); err != nil {
		return zero, err
	}
	if err := s.lockDefectContentProvenance(ctx, tx, who, plan, content); err != nil {
		return zero, err
	}
	outcome, run, err := v2input.BeginAdmissionInTransaction(ctx, tx, plan.shell, plan.key, plan.request.RequestSHA)
	if err != nil {
		return zero, err
	}
	if run != plan.shell.Run || (outcome != "created" && outcome != "pending") {
		return zero, v2input.ErrConflict
	}
	return DraftAdmission{RunID: run, Outcome: outcome}, nil
}
