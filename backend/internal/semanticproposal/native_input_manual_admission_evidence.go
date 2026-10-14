package semanticproposal

import (
	"context"
	"encoding/json"
	"reflect"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Distinct complete packet. Existing bare-native SQL must not receive this
// packet: admission requires an explicit SQL composition of manual evidence.
type nativeInputManualAdmissionEvidence struct {
	Schema     int                           `json:"schema_version"`
	Native     nativeDefectAdmissionEvidence `json:"native"`
	Manual     []json.RawMessage             `json:"manual_evidence"`
	ParamsText string                        `json:"params_text"`
}

func (s SQLTransferSourceReader) marshalNativeInputManualAdmissionEvidence(ctx context.Context, who auth.Identity, plan nativeInputManualDraftPlan, built v2input.Built) (json.RawMessage, error) {
	if err := verifyNativeInputManualDraftSeal(ctx, plan); err != nil {
		return nil, err
	}
	owned, err := s.compileNativeInputManualDraft(ctx, who, plan.prepared.stage)
	if err != nil {
		return nil, err
	}
	if owned.seal != plan.seal || !reflect.DeepEqual(owned.shell, plan.shell) {
		return nil, v2input.ErrConflict
	}
	stage := owned.prepared.stage
	return s.marshalNativeInputManualAdmissionPacket(ctx, who, owned.shell, owned.prepared.frozen, stage.dependencies, stage.qualification, stage.params, built)
}

// Callers independently recompile their initial or restored authority before sharing packet assembly.
func (s SQLTransferSourceReader) marshalNativeInputManualAdmissionPacket(ctx context.Context, who auth.Identity, shell v2input.DraftShell, frozen v2input.FrozenSemanticDefectTask, dependencies nativeInputManualDependencies, qualified nativeInputManualStageQualification, paramsObject defectEvidenceObject, built v2input.Built) (json.RawMessage, error) {
	q := qualified.qualification
	selected, err := selectQualificationPolicies(q.policy, []string{"defects.propose"})
	if err != nil {
		return nil, err
	}
	qualification, err := s.marshalDefectQualificationPreparation(ctx, who, shell.Tenant, q.report, selected["defects.propose"])
	if err != nil {
		return nil, err
	}
	registration, err := marshalNativeDefectTaskRegistrationBinding(ctx, frozen, shell, built)
	if err != nil {
		return nil, err
	}
	obj := paramsObject.object
	if obj.Bytes != int64(len(dependencies.selection.params.params)) || obj.SHA256 != paramsObject.ref.SHA256 || obj.Key == "" || obj.VersionID == "" || obj.VersionID == "null" || obj.ContentType != "application/json" {
		return nil, v2input.ErrInvalid
	}
	params, err := canonical.Marshal(map[string]any{"bucket": s.Bucket, "key": obj.Key, "version": obj.VersionID, "sha256": obj.SHA256, "bytes": obj.Bytes, "media_type": obj.ContentType})
	if err != nil || len(params) > 16384 {
		return nil, v2input.ErrInvalid
	}
	manual := make([]json.RawMessage, len(dependencies.selection.proofs))
	for i, proof := range dependencies.selection.proofs {
		manual[i], err = marshalManualEvidencePins(proof.original(), s.Bucket)
		if err != nil {
			return nil, err
		}
	}
	native := nativeDefectAdmissionEvidence{SchemaVersion: 1, ConfigText: string(shell.Config), Landmarks: json.RawMessage(dependencies.landmarks), Skin: json.RawMessage(dependencies.skin), Policy: json.RawMessage(q.policy.body), Qualification: json.RawMessage(q.report.expected), QualificationPin: qualification, ParamsPin: params, Registration: registration}
	packet, err := canonical.Marshal(nativeInputManualAdmissionEvidence{Schema: 1, Native: native, Manual: manual, ParamsText: dependencies.selection.params.params})
	if err != nil || len(packet) > 32<<20 {
		return nil, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return packet, nil
}
