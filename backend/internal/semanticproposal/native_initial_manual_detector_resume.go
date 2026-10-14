package semanticproposal

import (
	"context"
	"reflect"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Caller holds the original parent lock. Only original saved anchor, report,
// limits and immutable selection may be replayed; no new-start fallback exists.
func (b nativePreparationBootstrap) resumeExistingInitialManualDetector(ctx context.Context, who auth.Identity, request Request, key string, capacity TransferPreparation, reserved int64, documents v2input.DocumentPublisher, pixels []v2input.Ref, diameter [2]float64) (detectorDraftPlan, v2input.JobTicket, error) {
	var zero detectorDraftPlan
	if err := b.validate(ctx); err != nil {
		return zero, v2input.JobTicket{}, err
	}
	if documents == nil {
		return zero, v2input.JobTicket{}, v2input.ErrInvalid
	}
	if err := validateInitialManualPixelSelection(pixels); err != nil {
		return zero, v2input.JobTicket{}, err
	}
	if err := validateInitialManualDiameter(diameter); err != nil {
		return zero, v2input.JobTicket{}, err
	}
	capacity = ownTransferPreparation(capacity)
	if _, err := initialNativeFaceBudget(capacity, reserved, v2qualification.Metrics{}); err != nil {
		return zero, v2input.JobTicket{}, err
	}
	owned, err := ownNativePlanRequest(ctx, who, request, key)
	if err != nil {
		return zero, v2input.JobTicket{}, err
	}
	selected := append([]v2input.Ref(nil), pixels...)
	anchor, err := b.Reader.loadNativePreparationAnchor(ctx, who, owned.TenantID, owned.RunID, capacity.Color, capacity.MetadataBytes)
	if err != nil {
		return zero, v2input.JobTicket{}, err
	}
	if anchor.Key != key || !reflect.DeepEqual(anchor.Request, owned) {
		return zero, v2input.JobTicket{}, v2input.ErrConflict
	}
	plan, err := b.Reader.restoreDetectorDraftPlan(ctx, b.Catalog, who, anchor.Request, anchor.Key, anchor.Published, anchor.Snapshot, anchor.SnapshotBody, anchor.Color)
	if err != nil {
		return zero, v2input.JobTicket{}, err
	}
	selection, err := b.Reader.readNativeManualSelection(ctx, who, plan)
	if err != nil {
		return zero, v2input.JobTicket{}, err
	}
	if !reflect.DeepEqual(selection.Pixels, selected) || selection.Diameter != diameter {
		return zero, v2input.JobTicket{}, v2input.ErrConflict
	}
	state, err := b.Reader.readNativeInputSavedStageState(ctx, who, owned, anchor.Published.Ref, anchor.Snapshot, "face.detect")
	if err != nil {
		return zero, v2input.JobTicket{}, err
	}
	phase, err := nativeInputStagePreparationPhase(state)
	if err != nil {
		return zero, v2input.JobTicket{}, err
	}
	if phase == nativePreparationFailed {
		return zero, v2input.JobTicket{}, v2input.ErrConflict
	}
	if phase == nativePreparationBlocked {
		publisher := v2input.ScopedPublisher{Documents: documents, Tenant: owned.TenantID, Run: plan.shell.Run}
		if _, err := b.Reader.persistDetectorPreparationInputs(ctx, b.Scheduler, publisher, who, plan, anchor.Color); err != nil {
			return zero, v2input.JobTicket{}, err
		}
	}
	ticket, err := b.Reader.resumeInitialDetectorWithManualSelection(ctx, b.Scheduler, who, plan, selection.Pixels, selection.Diameter)
	if err != nil {
		return zero, v2input.JobTicket{}, err
	}
	if ticket.RunID != plan.shell.Run || ticket.WorkflowID != plan.shell.WorkflowID || ticket.State != "queued" {
		return zero, v2input.JobTicket{}, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, v2input.JobTicket{}, err
	}
	return plan, ticket, nil
}
