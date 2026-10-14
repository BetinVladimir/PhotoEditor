package semanticproposal

import (
	"context"
	"encoding/json"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Retains the complete original qualified parser/source proof, physical result
// pins and exact skin registration. This packet is data, not admission authority.
type nativeSkinAdmissionEvidence struct {
	SchemaVersion int             `json:"schema_version"`
	Parser        json.RawMessage `json:"parser"`
	ParserResult  json.RawMessage `json:"parser_result"`
	ConfigText    string          `json:"config_text"`
	ParamsPin     json.RawMessage `json:"params_pin"`
	Registration  json.RawMessage `json:"registration"`
}

func (s SQLTransferSourceReader) marshalNativeSkinAdmissionEvidence(ctx context.Context, who auth.Identity, plan nativeSkinDraftPlan, built, parserBuilt, detectorBuilt v2input.Built) (json.RawMessage, error) {
	if err := verifyNativeSkinPlanSeal(ctx, plan); err != nil {
		return nil, err
	}
	owned, err := s.compileNativeSkinDraftPlan(ctx, who, plan.parser, plan.prepared, plan.shell.Bytes)
	if err != nil {
		return nil, err
	}
	if owned.seal != plan.seal {
		return nil, v2input.ErrConflict
	}
	parser, err := s.marshalNativeFaceAdmissionEvidence(ctx, who, owned.parser, owned.prepared.params.parser.detector, parserBuilt, detectorBuilt)
	if err != nil {
		return nil, err
	}
	result, err := s.marshalNativeSkinDependencyEvidence(ctx, who, owned)
	if err != nil {
		return nil, err
	}
	registration, err := marshalNativeSkinRegistrationBinding(ctx, owned, built)
	if err != nil {
		return nil, err
	}
	obj := owned.prepared.paramsObject.object
	size, err := owned.prepared.frozen.ParamsByteCount()
	if err != nil {
		return nil, err
	}
	if obj.Bytes != size || obj.SHA256 != owned.prepared.params.ref.SHA256 || obj.Key == "" || obj.VersionID == "" || obj.VersionID == "null" || obj.ContentType != "application/json" {
		return nil, v2input.ErrInvalid
	}
	pin, err := canonical.Marshal(map[string]any{"bucket": s.Bucket, "key": obj.Key, "version": obj.VersionID, "sha256": obj.SHA256, "bytes": obj.Bytes, "media_type": obj.ContentType})
	if err != nil || len(pin) > 16384 {
		return nil, v2input.ErrInvalid
	}
	packet, err := canonical.Marshal(nativeSkinAdmissionEvidence{1, parser, result, string(owned.shell.Config), pin, registration})
	if err != nil || len(packet) > 32<<20 {
		return nil, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return packet, nil
}
