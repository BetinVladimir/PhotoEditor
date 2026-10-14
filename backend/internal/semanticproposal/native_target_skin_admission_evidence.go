package semanticproposal

import (
	"context"
	"encoding/json"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func (s SQLTransferSourceReader) marshalNativeTargetSkinAdmissionEvidence(ctx context.Context, who auth.Identity, original detectorDraftPlan, plan nativeTargetSkinDraftPlan, built, parserBuilt, detectorBuilt v2input.Built) (json.RawMessage, error) {
	if err := verifyNativeTargetSkinPlanSeal(ctx, plan); err != nil {
		return nil, err
	}
	owned, err := s.compileNativeTargetSkinDraftPlan(ctx, who, plan.parser, plan.prepared, plan.shell.Bytes)
	if err != nil {
		return nil, err
	}
	if owned.seal != plan.seal {
		return nil, v2input.ErrConflict
	}
	parser, err := s.marshalNativeTargetFaceAdmissionEvidence(ctx, who, original, owned.parser, parserBuilt, detectorBuilt)
	if err != nil {
		return nil, err
	}
	result, err := s.marshalNativeTargetSkinDependencyEvidence(ctx, who, owned)
	if err != nil {
		return nil, err
	}
	registration, err := marshalNativeTargetSkinRegistrationBinding(ctx, owned, built)
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
