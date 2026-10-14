package semanticproposal

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Pixels identify explicit original manual selections, not arbitrary historical
// masks. Manifest fields are reconstructed by the server and never accepted as
// producer evidence from a caller. No absent selection falls back to a bare path.
func (s SQLTransferSourceReader) selectInitialManualFacesFromPixels(ctx context.Context, who auth.Identity, producers nativeInputRestoredFaceProducers, color TransferColorProfile, pixels []v2input.Ref) ([]review.DefectProposalFaceInput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateInitialManualPixelSelection(pixels); err != nil {
		return nil, err
	}
	if err := verifyNativeInputFaceProducerPlans(ctx, who, producers); err != nil {
		return nil, err
	}
	owned := append([]v2input.Ref(nil), pixels...)
	s = s.withManualRasterScope()
	targets := make([]review.ProtectionMask, 0, len(owned))
	for _, pixel := range owned {
		target, err := s.readOriginalManualTarget(ctx, who, producers.detector.request.TenantID, producers.detector.request.Intent.Reference, pixel)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return s.selectInitialManualFaceInputs(ctx, who, producers, color, targets)
}
func validateInitialManualPixelSelection(pixels []v2input.Ref) error {
	if len(pixels) < 1 || len(pixels) > 64 {
		return v2input.ErrInvalid
	}
	seen := map[uuid.UUID]bool{}
	for _, pixel := range pixels {
		if pixel.ArtifactID == uuid.Nil || seen[pixel.ArtifactID] {
			return v2input.ErrInvalid
		}
		if _, err := domain.ParseDigest(pixel.SHA256); err != nil {
			return v2input.ErrInvalid
		}
		seen[pixel.ArtifactID] = true
	}
	return nil
}

func (b nativePreparationBootstrap) bootstrapPixelSelectedManualDefect(ctx context.Context, who auth.Identity, producers nativeInputRestoredFaceProducers, color TransferColorProfile, pixels []v2input.Ref, diameter [2]float64, capacity TransferPreparation, reserved int64, documents v2input.DocumentPublisher) (nativeInputManualInitialSubmission, error) {
	var zero nativeInputManualInitialSubmission
	if err := b.validate(ctx); err != nil {
		return zero, err
	}
	if documents == nil || capacity.Color.sha != color.sha {
		return zero, v2input.ErrInvalid
	}
	if _, err := initialNativeDefectBudget(capacity, reserved, v2qualification.Metrics{}); err != nil {
		return zero, err
	}
	b.Reader = b.Reader.withManualRasterScope()
	faces, err := b.Reader.selectInitialManualFacesFromPixels(ctx, who, producers, color, pixels)
	if err != nil {
		return zero, err
	}
	return b.bootstrapQualifiedManualDefect(ctx, who, producers, color, faces, diameter, capacity, reserved, documents)
}
