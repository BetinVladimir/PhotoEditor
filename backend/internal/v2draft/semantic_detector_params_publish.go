package v2draft

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// PublishSemanticDetectorParams derives canonical SFD Params from exact source
// bytes and uses the existing accepted-version draft quota/fencing lifecycle.
// Deployment/source qualification and final admission locks remain separate.
func (p Publisher) PublishSemanticDetectorParams(ctx context.Context, who auth.Identity, tenant, document uuid.UUID, binding v2input.SessionVersionBinding, imageBody json.RawMessage, modelLock string, score float64, maxFaces int) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	if tenant == uuid.Nil || document == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	prepared, err := v2input.PrepareSemanticFaceDetectParams(ctx, binding, imageBody, modelLock, score, maxFaces)
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
