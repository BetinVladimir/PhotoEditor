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
type nativeFaceAdmissionEvidence struct {
	SchemaVersion  int             `json:"schema_version"`
	Face           json.RawMessage `json:"face"`
	Detector       json.RawMessage `json:"detector"`
	DetectorResult json.RawMessage `json:"detector_result"`
}

func (s SQLTransferSourceReader) marshalNativeFaceAdmissionEvidence(ctx context.Context, who auth.Identity, plan nativeFaceDraftPlan, completed completedNativeDetector, built, detectorBuilt v2input.Built) (json.RawMessage, error) {
	if err := verifyNativeFacePlanSeal(ctx, plan); err != nil {
		return nil, err
	}
	rebuilt, err := s.compileNativeFaceDraftPlan(ctx, who, plan.detector, plan.prepared, plan.shell.Bytes)
	if err != nil {
		return nil, err
	}
	if rebuilt.seal != plan.seal || completed.catalog != plan.prepared.detector {
		return nil, v2input.ErrConflict
	}
	params, report, err := marshalNativeFaceDraftPins(ctx, plan)
	if err != nil {
		return nil, err
	}
	registration, err := marshalNativeFaceRegistrationBinding(ctx, plan, built)
	if err != nil {
		return nil, err
	}
	parent := plan.detector
	detector, err := s.marshalDetectorAdmissionEvidence(ctx, who, parent, detectorBuilt)
	if err != nil {
		return nil, err
	}
	result, err := s.marshalCompletedDetectorLockEvidence(ctx, who, parent, completed)
	if err != nil {
		return nil, err
	}
	face, err := s.marshalNativeAdmissionEvidence(ctx, who, parent.request, plan.shell, parent.anchor, parent.policy, parent.prepared.source, plan.prepared.operation, plan.prepared.document, plan.prepared.expected, report, params, json.RawMessage(plan.prepared.inputs), registration)
	if err != nil {
		return nil, err
	}
	packet, err := canonical.Marshal(nativeFaceAdmissionEvidence{1, face, detector, result})
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
