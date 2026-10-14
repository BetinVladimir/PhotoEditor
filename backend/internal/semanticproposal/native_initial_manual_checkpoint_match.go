package semanticproposal

import (
	"context"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Recompile exact Params from the immutable selection and original physical
// producers. Comparing the complete frozen payload also excludes extra manual,
// permanent or additional protection inputs on this explicit-selection branch.
func (s SQLTransferSourceReader) verifyCheckpointManualDefectSelection(ctx context.Context, who auth.Identity, producers nativeInputRestoredFaceProducers, color TransferColorProfile, plan nativeInputManualInitialDraftPlan) error {
	if err := verifyNativeInputManualInitialDraftSeal(ctx, plan); err != nil {
		return err
	}
	if err := verifyNativeInputFaceProducerPlans(ctx, who, producers); err != nil {
		return err
	}
	selection, err := s.readNativeManualSelection(ctx, who, producers.detector)
	if err != nil {
		return err
	}
	s = s.withManualRasterScope()
	faces, err := s.selectInitialManualFacesFromPixels(ctx, who, producers, color, selection.Pixels)
	if err != nil {
		return err
	}
	expected, err := s.prepareNativeInputManualInitialParams(ctx, who, producers, color, faces, selection.Diameter)
	if err != nil {
		return err
	}
	original := plan.prepared.stage.dependencies.dependencies
	if original.producers.detector.seal != producers.detector.seal || original.producers.landmarks.seal != producers.landmarks.seal || original.producers.parser.seal != producers.parser.seal || original.producers.skin.seal != producers.skin.seal {
		return v2input.ErrConflict
	}
	if expected.selection.params != original.selection.params {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
