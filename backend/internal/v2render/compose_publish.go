package v2render

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

type CompositionParamsPublisher interface {
	Publish(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, []byte) (v2input.Ref, error)
}

// PublishCompositionParams uses the existing immutable draft-upload lifecycle.
// Its document ID is stable for one task across retries; conflicting params
// cannot overwrite that task's first publication. Parent scope/admission remain
// separate checks. Returned request owns its nullable refs and mask bytes.
func PublishCompositionParams(ctx context.Context, request CompositionPreparation, publisher CompositionParamsPublisher) (CompositionPreparation, error) {
	if err := ctx.Err(); err != nil {
		return CompositionPreparation{}, err
	}
	if publisher == nil || request.Task == uuid.Nil || request.Tenant == uuid.Nil || request.Actor.UserID == uuid.Nil || request.Actor.SessionID == uuid.Nil {
		return CompositionPreparation{}, v2input.ErrInvalid
	}
	compiled, err := CompileComposition(request.Layer, request.Base, request.SourceResult, request.ProposalResult, request.ProtectedMask, request.Width, request.Height)
	if err != nil {
		return CompositionPreparation{}, err
	}
	document := uuid.NewSHA1(request.Task, []byte("layer.compose.params:1"))
	ref, err := publisher.Publish(ctx, request.Actor, request.Tenant, request.Base.AssetVersionID, document, compiled.JSON)
	if err != nil {
		return CompositionPreparation{}, err
	}
	sum := sha256.Sum256(compiled.JSON)
	if !validLayerArtifactRef(ref) || ref.SHA256 != hex.EncodeToString(sum[:]) {
		return CompositionPreparation{}, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return CompositionPreparation{}, err
	}
	clone := func(ref *v2input.Ref) *v2input.Ref {
		if ref == nil {
			return nil
		}
		owned := *ref
		return &owned
	}
	request.SourceResult = clone(compiled.Params.SourceResult)
	request.ProtectedMask = clone(compiled.Params.ProtectedMask)
	request.Layer.Mask = append([]byte(nil), compiled.Params.Mask...)
	request.ParamsRef = ref
	return request, nil
}
