package semanticproposal

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

type defectAdmissionAnchor struct {
	IntentText   string `json:"intent_text"`
	SnapshotText string `json:"snapshot_text"`
}

type defectAdmissionEvidence struct {
	Anchor         defectAdmissionAnchor `json:"anchor"`
	SchemaVersion  int                   `json:"schema_version"`
	Tenant         uuid.UUID             `json:"tenant_id"`
	Project        uuid.UUID             `json:"project_id"`
	Actor          uuid.UUID             `json:"actor_id"`
	Session        uuid.UUID             `json:"session_id"`
	Run            uuid.UUID             `json:"run_id"`
	ParentRun      uuid.UUID             `json:"parent_run_id"`
	RequestSHA     string                `json:"request_sha"`
	ConfigText     string                `json:"config_text"`
	PolicyText     string                `json:"policy_text"`
	ParamsDocument uuid.UUID             `json:"params_document_id"`
	Source         json.RawMessage       `json:"source_authority"`
	Origin         json.RawMessage       `json:"origin_pins"`
	OriginRefs     json.RawMessage       `json:"origin_refs"`
	Files          json.RawMessage       `json:"files"`
	ParamsPin      json.RawMessage       `json:"params_pin"`
	Content        json.RawMessage       `json:"content"`
	Faces          json.RawMessage       `json:"faces"`
	Parser         json.RawMessage       `json:"parser"`
	Manual         json.RawMessage       `json:"manual"`
	Person         json.RawMessage       `json:"person"`
	Registration   json.RawMessage       `json:"registration"`
}

// Owns a coherent packet from private retained evidence only. No DB/object IO
// or queued authority. Full SQL cross-binding, actual coverage and atomic
// graph activation still require their independent validation contracts.
func (s SQLTransferSourceReader) marshalDefectAdmissionEvidence(ctx context.Context, who auth.Identity, prepared defectPersonAdmissionPreparation, built v2input.Built) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	plan := prepared.plan
	if err := validatePreparedDefectSelection(plan.shell.Config, plan.originSelection, plan.execution.execution.face.originProof.pins.Params); err != nil {
		return nil, err
	}
	intentBody, err := canonical.Marshal(plan.request.Intent)
	if err != nil || string(intentBody) != string(plan.request.Body) {
		return nil, v2input.ErrInvalid
	}
	requestSHA, err := canonical.Hash(struct {
		Tenant uuid.UUID                  `json:"tenant_id"`
		Actor  uuid.UUID                  `json:"actor_id"`
		Intent v2input.SemanticEditIntent `json:"intent"`
	}{plan.request.TenantID, plan.request.ActorID, plan.request.Intent})
	if err != nil || requestSHA != plan.request.RequestSHA {
		return nil, v2input.ErrInvalid
	}
	anchorTargets, err := BindTargets(plan.request, plan.anchor.snapshotRef, plan.anchor.snapshotBody)
	if err != nil || len(anchorTargets) != len(plan.published.Inventory.Targets) || len(anchorTargets) != plan.published.Inventory.Scope.ItemCount {
		return nil, v2input.ErrInvalid
	}
	for index, target := range anchorTargets {
		if target != plan.published.Inventory.Targets[index] {
			return nil, v2input.ErrInvalid
		}
	}
	tenant := plan.request.TenantID
	binding := plan.request.Intent.Reference
	run, workflow, err := defectPreparationIDs(plan.request.RunID)
	if err != nil || run != plan.shell.Run || workflow != plan.shell.WorkflowID || plan.shell.Tenant != tenant || plan.shell.Project != s.ProjectID || plan.shell.Actor != who.UserID || plan.request.ActorID != who.UserID {
		return nil, v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(plan.request.RequestSHA); err != nil {
		return nil, v2input.ErrInvalid
	}
	if plan.key != "defect-prep:"+run.String() || len(plan.shell.Tasks) != 1 || len(plan.shell.Edges) != 0 || plan.shell.Workflow != "semantic" {
		return nil, v2input.ErrInvalid
	}
	task, err := plan.frozen.DraftTask()
	if err != nil {
		return nil, err
	}
	if task.ParamsArtifactID != plan.params.params.ArtifactID || task.ParamsSHA != plan.params.params.SHA256 {
		return nil, v2input.ErrInvalid
	}
	frozenTask, err := canonical.Marshal(task)
	if err != nil {
		return nil, err
	}
	shellTask, err := canonical.Marshal(plan.shell.Tasks[0])
	if err != nil || string(frozenTask) != string(shellTask) {
		return nil, v2input.ErrInvalid
	}
	configText, err := canonical.Bytes(plan.shell.Config)
	if err != nil || string(configText) != string(plan.shell.Config) {
		return nil, v2input.ErrInvalid
	}
	configSHA, err := canonical.Hash(json.RawMessage(configText))
	if err != nil || configSHA != plan.shell.ConfigSHA {
		return nil, v2input.ErrInvalid
	}
	policyText, err := canonical.Bytes([]byte(plan.execution.policy.body))
	if err != nil {
		return nil, err
	}
	policySHA, err := canonical.Hash(json.RawMessage(policyText))
	if err != nil || policySHA != plan.shell.PolicySHA {
		return nil, v2input.ErrInvalid
	}
	person, err := s.marshalDefectPersonAuthority(ctx, who, prepared)
	if err != nil {
		return nil, err
	}
	registration, err := marshalDefectRegistrationBinding(ctx, plan, built)
	if err != nil {
		return nil, err
	}
	source, err := s.marshalDefectSourceAuthority(ctx, who, tenant, binding, plan.execution)
	if err != nil {
		return nil, err
	}
	origin, err := s.marshalDefectOriginEvidencePins(who, tenant, binding, plan.execution.execution.face.originProof)
	if err != nil {
		return nil, err
	}
	refs, files, err := s.marshalDefectDeclaredFileAuthority(ctx, who, tenant, binding, plan.execution.execution.face)
	if err != nil {
		return nil, err
	}
	content, err := marshalDefectActivationContent(ctx, plan)
	if err != nil {
		return nil, err
	}
	faces, err := s.marshalDefectFaceAuthority(ctx, who, tenant, binding, plan.execution.execution.face)
	if err != nil {
		return nil, err
	}
	parser, err := s.marshalDefectParserAuthority(ctx, who, tenant, binding, plan.execution.execution)
	if err != nil {
		return nil, err
	}
	manual, err := s.marshalDefectManualAuthority(ctx, who, tenant, binding, plan.execution.execution)
	if err != nil {
		return nil, err
	}
	if err := validateDefectAdmissionClosure(files, content, registration); err != nil {
		return nil, fmt.Errorf("defect admission input closure: %w", err)
	}
	if err := validateDefectLandmarkInventory(files, content, faces); err != nil {
		return nil, fmt.Errorf("defect admission landmark inventory: %w", err)
	}
	if err := validateDefectMaskRootInventory(content, parser); err != nil {
		return nil, fmt.Errorf("defect admission mask root inventory: %w", err)
	}
	object := plan.params.object
	if plan.params.document == uuid.Nil || object.SHA256 != plan.params.params.SHA256 || object.Bytes != int64(len(plan.params.preparation.params.params)) || object.Key == "" || object.VersionID == "" || object.VersionID == "null" || object.ContentType != "application/json" {
		return nil, v2input.ErrInvalid
	}
	pin, err := canonical.Marshal(map[string]any{"key": object.Key, "version": object.VersionID, "sha256": object.SHA256, "bytes": object.Bytes, "media_type": object.ContentType, "bucket": s.Bucket})
	if err != nil {
		return nil, err
	}
	body, err := canonical.Marshal(defectAdmissionEvidence{Anchor: defectAdmissionAnchor{IntentText: string(plan.request.Body), SnapshotText: string(plan.anchor.snapshotBody)}, SchemaVersion: 1, ConfigText: string(configText), PolicyText: string(policyText), Tenant: tenant, Project: s.ProjectID, Actor: who.UserID, Session: who.SessionID, Run: run, ParentRun: plan.request.RunID, RequestSHA: plan.request.RequestSHA, ParamsDocument: plan.params.document, Source: source, Origin: origin, OriginRefs: refs, Files: files, ParamsPin: pin, Content: content, Faces: faces, Parser: parser, Manual: manual, Person: person, Registration: registration})
	if err != nil {
		return nil, err
	}
	if len(body) > 32<<20 {
		return nil, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return body, nil
}
