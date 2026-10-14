package semanticproposal

import (
	"context"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// A saved selection only locates the original manual inputs. Defect creation
// still verifies completed face producers and every physical manual closure.
// The caller holds the original parent lock and has established that no
// original defect child exists; restoration failures must never use this path.
func (b nativePreparationBootstrap) bootstrapCheckpointSelectedManualDefect(ctx context.Context, who auth.Identity, producers nativeInputRestoredFaceProducers, capacity TransferPreparation, reserved int64, documents v2input.DocumentPublisher) (nativeInputManualInitialSubmission, error) {
	var zero nativeInputManualInitialSubmission
	if err := b.validate(ctx); err != nil {
		return zero, err
	}
	if documents == nil {
		return zero, v2input.ErrInvalid
	}
	if _, err := initialNativeDefectBudget(capacity, reserved, v2qualification.Metrics{}); err != nil {
		return zero, err
	}
	if err := verifyNativeInputFaceProducerPlans(ctx, who, producers); err != nil {
		return zero, err
	}
	selection, err := b.Reader.readNativeManualSelection(ctx, who, producers.detector)
	if err != nil {
		return zero, err
	}
	return b.bootstrapPixelSelectedManualDefect(ctx, who, producers, capacity.Color, selection.Pixels, selection.Diameter, capacity, reserved, documents)
}
