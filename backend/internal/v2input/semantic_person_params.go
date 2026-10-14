package v2input

import (
	"context"
	"encoding/json"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
)

type SemanticPersonPreparation struct {
	Params json.RawMessage
	Inputs []Entry
}

// PrepareSemanticPersonParams builds independent LRASPP inputs without inferring
// person coverage from face/skin outputs. Scientific qualification, source
// authority, resource accounting and locked scheduling remain caller duties.
func PrepareSemanticPersonParams(ctx context.Context, binding SessionVersionBinding, imageBody json.RawMessage, modelLock string, guidedTile int) (SemanticPersonPreparation, error) {
	var zero SemanticPersonPreparation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if guidedTile != 256 && guidedTile != 512 && guidedTile != 1024 {
		return zero, ErrInvalid
	}
	if _, err := domain.ParseDigest(modelLock); err != nil {
		return zero, ErrInvalid
	}
	inputs, err := prepareSemanticFaceSourceInputs(ctx, binding, imageBody)
	if err != nil {
		return zero, err
	}
	params, err := canonical.Marshal(semanticPersonParams{Image: binding.Source, Model: "lraspp_mobilenet_v3_large", Lock: modelLock, Preprocessing: "person_guided_v1", LongSide: 768, GuidedTile: guidedTile})
	if err != nil {
		return zero, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return SemanticPersonPreparation{Params: params, Inputs: inputs}, nil
}
