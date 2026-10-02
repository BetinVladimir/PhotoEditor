package v2draft

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// ResolveRegionInputs uses existing source/project/parent catalog gates for a
// frozen recipe's source and base style. The FrozenRecipe publication itself
// has a separate authority gate; this method does not authorize that document.
func (s ClosureResolver) ResolveRegionInputs(ctx context.Context, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, style v2input.Ref, delta CorrectionVector) ([]v2input.Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if binding.AssetVersionID == uuid.Nil || !isDocumentRef(binding.Source) || !isDocumentRef(style) || !isDocumentRef(v2input.Ref{ArtifactID: binding.AssetVersionID, SHA256: binding.CanvasHash}) {
		return nil, v2input.ErrInvalid
	}
	styleRaw, err := s.readScopedContract(ctx, who, tenant, LayerRef{Path: "RegionPreview.base_style", Contract: "StyleV2", Ref: style})
	if err != nil {
		return nil, err
	}
	baseline, err := DecodeStyleBaseline(styleRaw)
	if err != nil || CheckStyleBounds(baseline, delta) != nil {
		return nil, v2input.ErrInvalid
	}
	roots := []LayerRef{
		{Path: "RegionPreview.source", Contract: "WorkingImage", Ref: binding.Source, CanvasHash: binding.CanvasHash},
		{Path: "RegionPreview.base_style", Contract: "StyleV2", Ref: style},
	}
	var inventory []v2input.Entry
	if _, err := s.resolveRootsWithInventory(ctx, who, tenant, roots, binding, &inventory); err != nil {
		return nil, err
	}
	return uniqueTaskInputs(inventory)
}
