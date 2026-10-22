package v2draft

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// ResolveAuthoredRegionInputs is a closed typed-root traversal for a current
// authored loupe. Catalog must be bound to its exact request, render task and
// canonical adoption; this method cannot authorize generic project artifacts.
// It preserves the full FrozenRenderRecipe (including layers/protection), and
// actual ImageResult/masks/report dependencies instead of scanning arbitrary JSON.
func (s ClosureResolver) ResolveAuthoredRegionInputs(ctx context.Context, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, recipe, result v2input.Ref) ([]v2input.Entry, error) {
	if tenant == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || binding.AssetVersionID == uuid.Nil || !isDocumentRef(binding.Source) || !isDocumentRef(recipe) || !isDocumentRef(result) || recipe.ArtifactID == result.ArtifactID || recipe.ArtifactID == binding.Source.ArtifactID || result.ArtifactID == binding.Source.ArtifactID {
		return nil, v2input.ErrInvalid
	}
	roots := []LayerRef{
		{Path: "AuthoredRegion.source", Contract: "WorkingImage", Ref: binding.Source, CanvasHash: binding.CanvasHash},
		{Path: "AuthoredRegion.frozen_recipe", Contract: "FrozenRenderRecipe", Ref: recipe},
		{Path: "AuthoredRegion.render_result", Contract: "ImageResult", Ref: result},
	}
	var inventory []v2input.Entry
	if _, err := s.resolveRootsWithInventory(ctx, who, tenant, roots, binding, &inventory); err != nil {
		return nil, err
	}
	entries, err := uniqueTaskInputs(inventory)
	if err != nil {
		return nil, err
	}
	// The existing full-render closure bound is retained. Leave one slot for the
	// new Params document; no bound on existing scalar region requests changes.
	if len(entries) < 5 || len(entries) >= maxRenderDocumentClosure {
		return nil, v2input.ErrInvalid
	}
	return entries, nil
}
