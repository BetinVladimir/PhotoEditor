package semanticproposal

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// The downstream stage retains both its own original proof and the complete
// original detector authority. A result ref alone cannot delegate admission.
// SQL composition/sealing is a separate gate; this packet activates nothing.
type nativeTargetFaceAdmissionEvidence struct {
	SchemaVersion  int             `json:"schema_version"`
	Face           json.RawMessage `json:"face"`
	Detector       json.RawMessage `json:"detector"`
	DetectorResult json.RawMessage `json:"detector_result"`
}

func (s SQLTransferSourceReader) marshalNativeTargetFaceAdmissionEvidence(ctx context.Context, who auth.Identity, original detectorDraftPlan, plan nativeTargetFaceDraftPlan, built, detectorBuilt v2input.Built) (json.RawMessage, error) {
	if err := verifyNativeTargetFacePlanSeal(ctx, plan); err != nil {
		return nil, err
	}
	rebuilt, err := s.compileNativeTargetFaceDraftPlan(ctx, who, plan.detector, plan.result, plan.prepared, plan.shell.Bytes)
	if err != nil {
		return nil, err
	}
	if rebuilt.seal != plan.seal || plan.result.completed.catalog != plan.prepared.detector {
		return nil, v2input.ErrConflict
	}
	params, report, err := marshalNativeFacePreparationPins(ctx, plan.prepared)
	if err != nil {
		return nil, err
	}
	registration, err := marshalNativeTargetFaceRegistrationBinding(ctx, plan, built)
	if err != nil {
		return nil, err
	}
	parent := plan.detector
	detector, err := s.marshalNativeTargetDetectorAdmissionEvidence(ctx, who, original, parent, detectorBuilt)
	if err != nil {
		return nil, err
	}
	result, err := s.marshalNativeTargetDetectorResultPins(ctx, who, parent, plan.result)
	if err != nil {
		return nil, err
	}
	pins, err := marshalSourceEvidencePins(parent.detector.prepared.source)
	if err != nil {
		return nil, err
	}
	face, err := canonical.Marshal(nativeAdmissionEvidence{SchemaVersion: 1, Operation: plan.prepared.operation, Tenant: plan.shell.Tenant, Project: s.ProjectID, Actor: who.UserID, Session: who.SessionID, Run: plan.shell.Run, ParentRun: original.request.RunID, RequestSHA: original.request.RequestSHA, ConfigText: string(plan.shell.Config), PolicyText: parent.detector.policy.body, Anchor: defectAdmissionAnchor{IntentText: string(original.request.Body), SnapshotText: string(original.anchor.snapshotBody)}, Binding: parent.anchor.entry.Binding, ICCSHA: parent.detector.prepared.source.iccSHA, UnitRGB: parent.detector.prepared.source.unitRGB, SourcePins: pins, Expected: json.RawMessage(plan.prepared.expected), Qualification: report, ParamsDocument: plan.prepared.document, ParamsPin: params, Files: json.RawMessage(plan.prepared.inputs), Registration: registration})
	if err != nil {
		return nil, err
	}
	packet, err := canonical.Marshal(nativeTargetFaceAdmissionEvidence{1, face, detector, result})
	if err != nil {
		return nil, err
	}
	if len(packet) > 32<<20 {
		return nil, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return packet, nil
}
