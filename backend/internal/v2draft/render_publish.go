package v2draft

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// PublishFrozenRenderRecipe publishes a complete validated wire through the
// existing immutable upload protocol, then registers SQL source provenance.
// Compilation, style/scalar authority and full-root catalog admission remain
// independent requirements; publication is not task authorization.
func (p Publisher) PublishFrozenRenderRecipe(ctx context.Context, who auth.Identity, tenantID, assetVersionID, documentID uuid.UUID, raw []byte) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	if len(raw) < 2 || len(raw) > 1048576 {
		return v2input.Ref{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	body := append([]byte(nil), raw...)
	recipe, err := DecodeFrozenRenderRecipe(body)
	if err != nil || assetVersionID == uuid.Nil || recipe.Scalars.Source.AssetVersionID != assetVersionID {
		return v2input.Ref{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	ref, err := p.Publish(ctx, who, tenantID, assetVersionID, documentID, body)
	if err != nil {
		return v2input.Ref{}, err
	}
	if err := p.RegisterFrozenRenderRecipe(ctx, who, tenantID, assetVersionID, documentID, ref, body); err != nil {
		return v2input.Ref{}, err
	}
	return ref, nil
}

// CompileAndPublishFrozenRenderRecipe derives the uploaded bytes from the full
// authored wire and trusted resolved inputs. It never accepts caller-authored
// frozen scalar values as a substitute for compiler output. SQL provenance and
// actual source/style/learning authorization remain independent required gates.
func (p Publisher) CompileAndPublishFrozenRenderRecipe(ctx context.Context, who auth.Identity, tenantID, documentID uuid.UUID, authored []byte, inputs ResolvedRecipeInputs, layerBody []byte) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	body, err := BuildFrozenRenderRecipe(authored, inputs, layerBody)
	if err != nil {
		return v2input.Ref{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	return p.PublishFrozenRenderRecipe(ctx, who, tenantID, inputs.Binding.AssetVersionID, documentID, body)
}
