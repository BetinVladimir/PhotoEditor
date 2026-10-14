package semanticproposal

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Separate target envelope: the original reference/request is never rewritten
// to reuse the reference-only admission compiler.
func (s SQLTransferSourceReader) marshalNativeTargetDetectorAdmissionEvidence(ctx context.Context, who auth.Identity, d detectorDraftPlan, plan nativeTargetDetectorPlan, built v2input.Built) (json.RawMessage, error) {
	if err := verifyNativeTargetDetectorPlan(ctx, plan); err != nil {
		return nil, err
	}
	if err := verifyDetectorPlanSeal(ctx, d); err != nil {
		return nil, err
	}
	expectedAnchor, err := compileNativeTargetAnchor(ctx, who, d.request, d.requestKey, d.published, d.anchor.snapshotRef, d.anchor.snapshotBody, plan.anchor.manifest, plan.anchor.entry.Ordinal, "face.detect")
	if err != nil {
		return nil, err
	}
	if expectedAnchor.seal != plan.anchor.seal {
		return nil, v2input.ErrConflict
	}
	rebuilt, err := s.compileNativeTargetDetectorPlan(ctx, who, plan.anchor, plan.detector.prepared, plan.detector.policy, plan.detector.shell.Bytes)
	if err != nil {
		return nil, err
	}
	if rebuilt.seal != plan.seal {
		return nil, v2input.ErrConflict
	}
	p := rebuilt.detector
	selected, err := selectQualificationPolicies(p.policy, []string{"face.detect"})
	if err != nil {
		return nil, err
	}
	report, err := s.marshalDetectorQualificationProof(ctx, who, plan.anchor.request.TenantID, p.prepared.qualification, selected["face.detect"])
	if err != nil {
		return nil, err
	}
	params, err := marshalDetectorParamsPin(ctx, s.Bucket, p.prepared)
	if err != nil {
		return nil, err
	}
	registration, err := marshalNativeRegistrationBinding(ctx, p.prepared.frozen, built, "face.detect")
	if err != nil {
		return nil, err
	}
	pins, err := marshalSourceEvidencePins(p.prepared.source)
	if err != nil {
		return nil, err
	}
	original := nativeAdmissionEvidence{SchemaVersion: 1, Operation: "face.detect", Tenant: d.request.TenantID, Project: s.ProjectID, Actor: who.UserID, Session: who.SessionID, Run: d.shell.Run, ParentRun: d.request.RunID, RequestSHA: d.request.RequestSHA, ConfigText: string(d.shell.Config), Anchor: defectAdmissionAnchor{IntentText: string(d.request.Body), SnapshotText: string(d.anchor.snapshotBody)}, Binding: d.request.Intent.Reference}
	target := nativeAdmissionEvidence{SchemaVersion: 1, Operation: "face.detect", Tenant: p.request.TenantID, Project: s.ProjectID, Actor: who.UserID, Session: who.SessionID, Run: p.shell.Run, ParentRun: p.request.RunID, RequestSHA: p.request.RequestSHA, ConfigText: string(p.shell.Config), PolicyText: p.policy.body, Anchor: original.Anchor, Binding: plan.anchor.entry.Binding, ICCSHA: p.prepared.source.iccSHA, UnitRGB: p.prepared.source.unitRGB, SourcePins: pins, Expected: json.RawMessage(p.prepared.qualification.expected), Qualification: report, ParamsDocument: p.prepared.document, ParamsPin: params, Files: json.RawMessage(p.prepared.inputs), Registration: registration}
	body, err := canonical.Marshal(struct {
		SchemaVersion int                     `json:"schema_version"`
		Original      nativeAdmissionEvidence `json:"original"`
		Target        nativeAdmissionEvidence `json:"target"`
	}{1, original, target})
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

// Caller owns original parent advisory lock and rollback. No object IO occurs here.
func (s SQLTransferSourceReader) sealNativeTargetDetectorInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, d detectorDraftPlan, plan nativeTargetDetectorPlan, built v2input.Built) (v2input.JobTicket, error) {
	evidence, err := s.marshalNativeTargetDetectorAdmissionEvidence(ctx, who, d, plan, built)
	if err != nil {
		return v2input.JobTicket{}, err
	}
	return sealNativePreparationQueryInTransaction(ctx, tx, plan.anchor.request.TenantID, plan.detector.shell.Run, plan.detector.shell.WorkflowID, evidence, "SELECT photo.seal_semantic_target_detector_preparation($1,$2,$3::jsonb)")
}
