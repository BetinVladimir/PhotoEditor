package v2draft

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// These publishers compile only original-source/SFD-bound Params. They do not
// qualify FAN/BiSeNet or grant permission to register or activate a task.
func (p Publisher) PublishSemanticLandmarkParams(ctx context.Context, who auth.Identity, tenant, document uuid.UUID, binding v2input.SessionVersionBinding, image json.RawMessage, faces v2input.Ref, facesBody json.RawMessage, detectorParams v2input.Ref, detectorBody json.RawMessage, model string) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	if !validNativeFaceParamsPublication(who, tenant, document, detectorParams) {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	prepared, err := v2input.PrepareSemanticLandmarkParams(ctx, binding, image, faces, facesBody, detectorParams, detectorBody, model)
	if err != nil {
		return v2input.Ref{}, err
	}
	for _, entry := range prepared.Inputs {
		if entry.Ref.ArtifactID == document {
			return v2input.Ref{}, v2input.ErrInvalid
		}
	}
	return p.publish(ctx, who, tenant, binding.AssetVersionID, document, prepared.Params, false, true)
}
func (p Publisher) PublishSemanticFaceParseParams(ctx context.Context, who auth.Identity, tenant, document uuid.UUID, binding v2input.SessionVersionBinding, image json.RawMessage, faces v2input.Ref, facesBody json.RawMessage, detectorParams v2input.Ref, detectorBody json.RawMessage, model string) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	if !validNativeFaceParamsPublication(who, tenant, document, detectorParams) {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	prepared, err := v2input.PrepareSemanticFaceParseParams(ctx, binding, image, faces, facesBody, detectorParams, detectorBody, model)
	if err != nil {
		return v2input.Ref{}, err
	}
	for _, entry := range prepared.Inputs {
		if entry.Ref.ArtifactID == document {
			return v2input.Ref{}, v2input.ErrInvalid
		}
	}
	return p.publish(ctx, who, tenant, binding.AssetVersionID, document, prepared.Params, false, true)
}
func validNativeFaceParamsPublication(who auth.Identity, tenant, document uuid.UUID, detectorParams v2input.Ref) bool {
	return who.UserID != uuid.Nil && who.SessionID != uuid.Nil && tenant != uuid.Nil && document != uuid.Nil && document != detectorParams.ArtifactID
}
