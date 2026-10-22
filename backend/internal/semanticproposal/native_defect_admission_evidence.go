package semanticproposal

import (
	"context"
	"encoding/json"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Data for a composed SQL admission gate, not permission to queue the task.
// Completed dependency packets were physically verified before opening SQL.
type nativeDefectAdmissionEvidence struct {
	SchemaVersion    int             `json:"schema_version"`
	ConfigText       string          `json:"config_text"`
	Landmarks        json.RawMessage `json:"landmarks"`
	Skin             json.RawMessage `json:"skin"`
	Policy           json.RawMessage `json:"policy"`
	Qualification    json.RawMessage `json:"qualification"`
	QualificationPin json.RawMessage `json:"qualification_pin"`
	ParamsPin        json.RawMessage `json:"params_pin"`
	Registration     json.RawMessage `json:"registration"`
	Person           json.RawMessage `json:"person,omitempty"`
}

func (s SQLTransferSourceReader) marshalNativeDefectAdmissionEvidence(ctx context.Context, who auth.Identity, plan nativeDefectDraftPlan, built v2input.Built) (json.RawMessage, error) {
	if err := verifyNativeDefectDraftSeal(ctx, plan); err != nil {
		return nil, err
	}
	owned, err := s.compileNativeDefectDraftPlan(ctx, who, plan.prepared, plan.shell.Bytes)
	if err != nil {
		return nil, err
	}
	if owned.seal != plan.seal {
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
	registration, err := marshalNativeDefectRegistrationBinding(ctx, owned, built)
	if err != nil {
		return nil, err
	}
	obj := p.params.object
	if obj.Bytes != int64(len(p.dependencies.preparation.params.params)) || obj.SHA256 != p.params.ref.SHA256 || obj.Key == "" || obj.VersionID == "" || obj.VersionID == "null" || obj.ContentType != "application/json" {
		return nil, v2input.ErrInvalid
	}
	pin, err := canonical.Marshal(map[string]any{"bucket": s.Bucket, "key": obj.Key, "version": obj.VersionID, "sha256": obj.SHA256, "bytes": obj.Bytes, "media_type": obj.ContentType})
	if err != nil || len(pin) > 16384 {
		return nil, v2input.ErrInvalid
	}
	var person json.RawMessage
	if p.dependencies.preparation.person != nil {
		if err := verifyNativeDefectPreparedParams(ctx, p.dependencies.preparation); err != nil {
			return nil, err
		}
		body, err := canonical.Marshal(nativePersonInputAuthorityPayload(*p.dependencies.preparation.person))
		if err != nil {
			return nil, err
		}
		person, err = canonical.Marshal(map[string]any{"authority_text": string(body), "params_text": p.dependencies.preparation.params.params})
		if err != nil {
			return nil, err
		}
	}
	packet, err := canonical.Marshal(nativeDefectAdmissionEvidence{SchemaVersion: 1, ConfigText: string(owned.shell.Config), Landmarks: json.RawMessage(p.dependencies.landmarks), Skin: json.RawMessage(p.dependencies.skin), Policy: json.RawMessage(p.qualification.policy.body), Qualification: json.RawMessage(p.qualification.report.expected), QualificationPin: qualificationPin, ParamsPin: pin, Registration: registration, Person: person})
	if err != nil || len(packet) > 32<<20 {
		return nil, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return packet, nil
}
