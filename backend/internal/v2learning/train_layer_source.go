package v2learning

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// TrainingLayerSource proves the exact LayerStack belongs to the edited asset
// before its bytes can decide whether an example is parametric.
type TrainingLayerSource struct {
	Catalog v2draft.ArtifactCatalog
	Objects LearningEvaluateObjectReader
}

func (s TrainingLayerSource) Load(ctx context.Context, who auth.Identity,
	tenantID uuid.UUID, ref v2input.Ref,
	binding v2input.SessionVersionBinding) ([]byte, bool, error) {
	if s.Catalog == nil || s.Objects == nil || tenantID == uuid.Nil ||
		!validManifestRef(ref) || binding.AssetVersionID == uuid.Nil ||
		!validManifestRef(binding.Source) || !validSHA(binding.CanvasHash) {
		return nil, false, ErrInvalidTrainingDraft
	}
	object, err := s.Catalog.Lookup(ctx, who, tenantID, v2draft.LayerRef{
		Path: "EditingRecipeV3.layer_stack", Contract: "LayerStack",
		Ref: ref})
	if err != nil {
		return nil, false, err
	}
	body, err := ReadFrozenRootDocument(ctx, s.Objects, object, ref)
	if err != nil {
		return nil, false, err
	}
	active, err := TrainingLayerActivity(body, binding)
	if err != nil {
		return nil, false, err
	}
	return body, active, nil
}
