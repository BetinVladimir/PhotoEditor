package semanticproposal

import (
	"context"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Internal child coordinator, never a public semantic parent receipt. The same
// original-parent session lock used by durable recovery covers presence, first
// publication and restoration. Lock contention returns no work and no receipt.
func (b nativePreparationBootstrap) startOrResumeInitialManualDetector(ctx context.Context, who auth.Identity, request Request, key string, capacity TransferPreparation, reserved int64, documents v2input.DocumentPublisher, score float64, maxFaces int, pixels []v2input.Ref, diameter [2]float64) (detectorDraftPlan, v2input.JobTicket, bool, error) {
	var plan detectorDraftPlan
	var ticket v2input.JobTicket
	if err := b.validate(ctx); err != nil {
		return plan, ticket, false, err
	}
	if documents == nil {
		return plan, ticket, false, v2input.ErrInvalid
	}
	if err := validateInitialDetectorControls(score, maxFaces); err != nil {
		return plan, ticket, false, err
	}
	if err := validateInitialManualPixelSelection(pixels); err != nil {
		return plan, ticket, false, err
	}
	if err := validateInitialManualDiameter(diameter); err != nil {
		return plan, ticket, false, err
	}
	capacity = ownTransferPreparation(capacity)
	if _, err := initialNativeFaceBudget(capacity, reserved, v2qualification.Metrics{}); err != nil {
		return plan, ticket, false, err
	}
	owned, err := ownNativePlanRequest(ctx, who, request, key)
	if err != nil {
		return plan, ticket, false, err
	}
	selected := append([]v2input.Ref(nil), pixels...)
	locked, err := withNativeManualParentLock(ctx, b.Scheduler, owned.TenantID, owned.RunID, func(work context.Context) error {
		presence, err := b.Reader.readInitialNativeDetectorPresence(work, who, owned, key)
		if err != nil {
			return err
		}
		switch presence {
		case nativeStageAbsent:
			plan, ticket, err = b.prepareAndStartNewInitialManualDetector(work, who, owned, key, capacity, reserved, documents, score, maxFaces, selected, diameter)
		case nativeStagePresent:
			plan, ticket, err = b.resumeExistingInitialManualDetector(work, who, owned, key, capacity, reserved, documents, selected, diameter)
		default:
			return v2input.ErrConflict
		}
		return err
	})
	if err != nil || !locked {
		return detectorDraftPlan{}, v2input.JobTicket{}, locked, err
	}
	if err := ctx.Err(); err != nil {
		return detectorDraftPlan{}, v2input.JobTicket{}, false, err
	}
	return plan, ticket, true, nil
}
