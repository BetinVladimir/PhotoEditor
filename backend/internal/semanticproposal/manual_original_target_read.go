package semanticproposal

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// A pixel ref is a locator only. Resolve its original canonical producer root,
// source and declaration before independently verifying all original manual
// Params/vectors/target/protected arrays through the existing physical reader.
func (s SQLTransferSourceReader) readOriginalManualTarget(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, pixel v2input.Ref) (review.ProtectionMask, error) {
	var zero review.ProtectionMask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	catalog, err := s.manualPreparationObjects(ctx, who, tenantID, binding, pixel)
	if err != nil {
		return zero, err
	}
	raw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, catalog.Root.object, catalog.Root.ref)
	if err != nil {
		return zero, err
	}
	sourceBody, err := s.Read(ctx, who, tenantID, binding)
	if err != nil {
		return zero, err
	}
	image, err := review.ParseWorkingImage(sourceBody)
	if err != nil {
		return zero, err
	}
	target, err := review.ResolveOriginalManualTarget(raw, review.ArtifactRef{ID: catalog.Root.ref.ArtifactID, SHA256: catalog.Root.ref.SHA256}, review.ArtifactRef{ID: pixel.ArtifactID, SHA256: pixel.SHA256}, image.Source(review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256}))
	if err != nil {
		return zero, err
	}
	original, proof, err := s.readManualPreparationProof(ctx, who, tenantID, binding, target)
	if err != nil {
		return zero, err
	}
	if proof.objects != catalog {
		return zero, v2input.ErrConflict
	}
	if err := s.recheckManualPreparationProof(ctx, who, tenantID, binding, proof); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return original.Target, nil
}
