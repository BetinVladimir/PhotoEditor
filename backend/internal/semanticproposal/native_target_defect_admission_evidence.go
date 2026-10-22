package semanticproposal

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Dedicated target envelope: person is the complete target admission packet,
// never the reference-only nativeDefectAdmissionEvidence person authority.
// Serialization does not authorize registration, activation or queueing.
type nativeTargetDefectAdmissionEvidence struct {
	SchemaVersion    int             `json:"schema_version"`
	ConfigText       string          `json:"config_text"`
	Person           json.RawMessage `json:"person"`
	Landmarks        json.RawMessage `json:"landmarks"`
	Skin             json.RawMessage `json:"skin"`
	Policy           json.RawMessage `json:"policy"`
	Qualification    json.RawMessage `json:"qualification"`
	QualificationPin json.RawMessage `json:"qualification_pin"`
	ParamsPin        json.RawMessage `json:"params_pin"`
	Registration     json.RawMessage `json:"registration"`
}

// No object IO or SQL transaction. Callers prepare physical proofs first and
// must repeat canonical dependency/qualification/pin locks at admission.
func (s SQLTransferSourceReader) marshalNativeTargetDefectAdmissionEvidence(ctx context.Context, who auth.Identity, original detectorDraftPlan, plan nativeTargetDefectDraftPlan, dependencies nativeTargetDefectDependencies, built v2input.Built) (json.RawMessage, error) {
	if err := verifyNativeTargetDefectPlanSeal(ctx, plan); err != nil {
		return nil, err
	}
	owned, err := s.compileNativeTargetDefectDraftPlan(ctx, who, plan.prepared, plan.shell.Bytes)
	if err != nil {
		return nil, err
	}
	if owned.seal != plan.seal {
		return nil, v2input.ErrConflict
	}
	digest, err := targetDefectDependencyDigest(dependencies)
	if err != nil {
		return nil, err
	}
	if dependencies.seal == "" || digest != dependencies.seal || dependencies.preparation.seal != owned.prepared.preparation.seal {
		return nil, v2input.ErrConflict
	}
	f := dependencies.preparation
	person, err := s.marshalNativeTargetPersonAdmissionEvidence(ctx, who, original, f.personPlan, dependencies.transports.person)
	if err != nil {
		return nil, err
	}
	landmarks, err := s.marshalNativeTargetFaceAdmissionEvidence(ctx, who, original, f.landmarksPlan, dependencies.transports.landmarks, dependencies.transports.detector)
	if err != nil {
		return nil, err
	}
	if string(person) != dependencies.person || string(landmarks) != dependencies.landmarks {
		return nil, v2input.ErrConflict
	}
	p := owned.prepared
	selected, err := selectNativeQualificationPolicies(p.qualification.policy, []string{"defects.propose"})
	if err != nil {
		return nil, err
	}
	qualificationPin, err := s.marshalDefectQualificationPreparation(ctx, who, owned.shell.Tenant, p.qualification.report, selected["defects.propose"])
	if err != nil {
		return nil, err
	}
	registration, err := marshalNativeTargetDefectRegistrationBinding(ctx, owned, built)
	if err != nil {
		return nil, err
	}
	obj := p.params.object
	if obj.Bytes != int64(len(f.params.params)) || obj.SHA256 != p.params.ref.SHA256 || obj.Key == "" || obj.VersionID == "" || obj.VersionID == "null" || obj.ContentType != "application/json" {
		return nil, v2input.ErrInvalid
	}
	pin, err := canonical.Marshal(map[string]any{"bucket": s.Bucket, "key": obj.Key, "version": obj.VersionID, "sha256": obj.SHA256, "bytes": obj.Bytes, "media_type": obj.ContentType})
	if err != nil || len(pin) > 16384 {
		return nil, v2input.ErrInvalid
	}
	packet, err := canonical.Marshal(nativeTargetDefectAdmissionEvidence{1, string(owned.shell.Config), person, landmarks, json.RawMessage(dependencies.skin), json.RawMessage(p.qualification.policy.body), json.RawMessage(p.qualification.report.expected), qualificationPin, pin, registration})
	if err != nil || len(packet) > 32<<20 {
		return nil, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return packet, nil
}
