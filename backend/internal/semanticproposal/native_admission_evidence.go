package semanticproposal

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// Native preparation carries original source authority only. It must not inherit
// authority from unrelated detector/person outputs or from a caller's task JSON.
// This packet is consumed by dedicated admission validation, not generic sealing.
type nativeAdmissionEvidence struct {
	SchemaVersion  int                           `json:"schema_version"`
	Operation      string                        `json:"operation"`
	Tenant         uuid.UUID                     `json:"tenant_id"`
	Project        uuid.UUID                     `json:"project_id"`
	Actor          uuid.UUID                     `json:"actor_id"`
	Session        uuid.UUID                     `json:"session_id"`
	Run            uuid.UUID                     `json:"run_id"`
	ParentRun      uuid.UUID                     `json:"parent_run_id"`
	RequestSHA     string                        `json:"request_sha"`
	ConfigText     string                        `json:"config_text"`
	PolicyText     string                        `json:"policy_text"`
	Anchor         defectAdmissionAnchor         `json:"anchor"`
	Binding        v2input.SessionVersionBinding `json:"binding"`
	ICCSHA         string                        `json:"icc_sha"`
	UnitRGB        bool                          `json:"unit_rgb"`
	SourcePins     json.RawMessage               `json:"source_pins"`
	Expected       json.RawMessage               `json:"expected"`
	Qualification  json.RawMessage               `json:"qualification"`
	ParamsDocument uuid.UUID                     `json:"params_document_id"`
	ParamsPin      json.RawMessage               `json:"params_pin"`
	Files          json.RawMessage               `json:"files"`
	Registration   json.RawMessage               `json:"registration"`
}

func (s SQLTransferSourceReader) marshalDetectorAdmissionEvidence(ctx context.Context, who auth.Identity, plan detectorDraftPlan, built v2input.Built) (json.RawMessage, error) {
	if err := verifyDetectorPlanSeal(ctx, plan); err != nil {
		return nil, err
	}
	rebuilt, err := s.recompileDetectorDraftPlan(ctx, who, plan)
	if err != nil {
		return nil, err
	}
	if rebuilt.seal != plan.seal {
		return nil, v2input.ErrConflict
	}
	if err := verifyDetectorSourceInputPins(ctx, plan.prepared); err != nil {
		return nil, err
	}
	selected, err := selectNativeQualificationPolicies(plan.policy, []string{"face.detect"})
	if err != nil {
		return nil, err
	}
	report, err := s.marshalDetectorQualificationProof(ctx, who, plan.request.TenantID, plan.prepared.qualification, selected["face.detect"])
	if err != nil {
		return nil, err
	}
	params, err := marshalDetectorParamsPin(ctx, s.Bucket, plan.prepared)
	if err != nil {
		return nil, err
	}
	registration, err := marshalDetectorRegistrationBinding(ctx, plan, built)
	if err != nil {
		return nil, err
	}
	return s.marshalNativeAdmissionEvidence(ctx, who, plan.request, plan.shell, plan.anchor, plan.policy, plan.prepared.source, "face.detect", plan.prepared.document, plan.prepared.qualification.expected, report, params, []byte(plan.prepared.inputs), registration)
}

func (s SQLTransferSourceReader) marshalPersonAdmissionEvidence(ctx context.Context, who auth.Identity, plan personDraftPlan, built v2input.Built) (json.RawMessage, error) {
	if err := verifyPersonPlanSeal(ctx, plan); err != nil {
		return nil, err
	}
	rebuilt, err := s.compilePersonDraftPlan(ctx, who, plan.request, plan.requestKey, plan.published, plan.anchor.snapshotRef, plan.anchor.snapshotBody, plan.prepared, plan.policy, plan.shell.Bytes)
	if err != nil {
		return nil, err
	}
	if rebuilt.seal != plan.seal {
		return nil, v2input.ErrConflict
	}
	if err := verifyPersonSourceInputPins(ctx, plan.prepared.catalog, plan.prepared.source); err != nil {
		return nil, err
	}
	selected, err := selectNativeQualificationPolicies(plan.policy, []string{"mask.person"})
	if err != nil {
		return nil, err
	}
	report, err := s.marshalPersonQualificationProof(ctx, who, plan.request.TenantID, plan.prepared.qualification, selected["mask.person"])
	if err != nil {
		return nil, err
	}
	params, err := marshalPersonParamsPin(ctx, s.Bucket, plan.prepared)
	if err != nil {
		return nil, err
	}
	registration, err := marshalPersonRegistrationBinding(ctx, plan, built)
	if err != nil {
		return nil, err
	}
	return s.marshalNativeAdmissionEvidence(ctx, who, plan.request, plan.shell, plan.anchor, plan.policy, plan.prepared.source, "mask.person", plan.prepared.document, plan.prepared.qualification.expected, report, params, []byte(plan.prepared.catalog.inputs), registration)
}

func (s SQLTransferSourceReader) marshalNativeAdmissionEvidence(ctx context.Context, who auth.Identity, request Request, shell v2input.DraftShell, anchor WorkflowPlan, policy qualificationPolicySnapshot, source sourcePreparationProof, operation string, document uuid.UUID, expected string, report, params, files, registration json.RawMessage) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if (operation != "face.detect" && operation != "mask.person" && operation != "face.landmarks" && operation != "face.parse") || !validQualificationPolicySnapshot(s, who, request.TenantID, policy) || source.tenant != request.TenantID || source.project != s.ProjectID || source.actor != who.UserID || source.session != who.SessionID || source.bucket != s.Bucket || source.binding != request.Intent.Reference || !source.unitRGB || document == uuid.Nil {
		return nil, v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(source.iccSHA); err != nil {
		return nil, v2input.ErrInvalid
	}
	pins, err := marshalSourceEvidencePins(source)
	if err != nil {
		return nil, err
	}
	config, err := canonical.Bytes(shell.Config)
	if err != nil || string(config) != string(shell.Config) {
		return nil, v2input.ErrInvalid
	}
	configSHA, err := canonical.Hash(json.RawMessage(config))
	if err != nil || configSHA != shell.ConfigSHA {
		return nil, v2input.ErrInvalid
	}
	policyText, err := canonical.Bytes([]byte(policy.body))
	if err != nil || string(policyText) != policy.body {
		return nil, v2input.ErrInvalid
	}
	policySHA, err := canonical.Hash(json.RawMessage(policyText))
	if err != nil || policySHA != shell.PolicySHA {
		return nil, v2input.ErrInvalid
	}
	// Retain canonical text so SQL can verify original byte hashes independently.
	body, err := canonical.Marshal(nativeAdmissionEvidence{SchemaVersion: 1, Operation: operation, Tenant: request.TenantID, Project: s.ProjectID, Actor: who.UserID, Session: who.SessionID, Run: shell.Run, ParentRun: request.RunID, RequestSHA: request.RequestSHA, ConfigText: string(config), PolicyText: string(policyText), Anchor: defectAdmissionAnchor{IntentText: string(request.Body), SnapshotText: string(anchor.snapshotBody)}, Binding: source.binding, ICCSHA: source.iccSHA, UnitRGB: source.unitRGB, SourcePins: pins, Expected: json.RawMessage(expected), Qualification: report, ParamsDocument: document, ParamsPin: params, Files: files, Registration: registration})
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
