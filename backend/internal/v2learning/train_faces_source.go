package v2learning

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type TrainingFacesSource struct {
	Catalog v2draft.ArtifactCatalog
	Objects LearningEvaluateObjectReader
}

// Load verifies the exact FaceObservations ref already pinned by the adopted
// descriptor. Empty faces are valid only in a canonical, source-bound document.
func (s TrainingFacesSource) Load(ctx context.Context, who auth.Identity,
	tenantID uuid.UUID, binding v2input.SessionVersionBinding,
	ref v2input.Ref) (int, error) {
	if s.Catalog == nil || s.Objects == nil || tenantID == uuid.Nil ||
		binding.AssetVersionID == uuid.Nil || !validManifestRef(binding.Source) ||
		!validSHA(binding.CanvasHash) || !validManifestRef(ref) {
		return 0, ErrInvalidTrainingDraft
	}
	object, err := s.Catalog.Lookup(ctx, who, tenantID, v2draft.LayerRef{
		Path: "DescriptorShard.frames.faces", Contract: "Artifact", Ref: ref})
	if err != nil {
		return 0, err
	}
	body, err := ReadFrozenRootDocument(ctx, s.Objects, object, ref)
	if err != nil {
		return 0, err
	}
	return ValidateFrozenFaceObservations(body, binding)
}
