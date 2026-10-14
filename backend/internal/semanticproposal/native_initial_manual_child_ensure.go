package semanticproposal

import (
	"context"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Caller holds the original parent lock. An existing child is always restored;
// failed restoration never grants permission to compile a replacement.
func (b nativePreparationBootstrap) ensureCheckpointSelectedManualDefect(ctx context.Context, who auth.Identity, producers nativeInputRestoredFaceProducers, capacity TransferPreparation, reserved int64, documents v2input.DocumentPublisher) (nativeInputManualInitialSubmission, error) {
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
	presence, err := b.Reader.readNativeStagePresence(ctx, who, producers.detector, "defects.propose")
	if err != nil {
		return zero, err
	}
	switch presence {
	case nativeStageAbsent:
		return b.bootstrapCheckpointSelectedManualDefect(ctx, who, producers, capacity, reserved, documents)
	case nativeStagePresent:
		plan, err := b.Reader.restoreNativeInputManualInitialPlan(ctx, b.Scheduler, who, producers, capacity.Color)
		if err != nil {
			return zero, err
		}
		if err := b.Reader.verifyCheckpointManualDefectSelection(ctx, who, producers, capacity.Color, plan); err != nil {
			return zero, err
		}
		d := producers.detector
		state, err := b.Reader.readNativeInputSavedStageState(ctx, who, d.request, d.published.Ref, d.anchor.snapshotRef, "defects.propose")
		if err != nil {
			return zero, err
		}
		phase, err := nativeInputStagePreparationPhase(state)
		if err != nil {
			return zero, err
		}
		if phase == nativePreparationFailed {
			return zero, v2input.ErrConflict
		}
		var ticket v2input.JobTicket
		if phase == nativePreparationBlocked {
			// Repeat publication only from the verified original unsealed plan.
			// Registration and complete admission still commit together.
			publisher := v2input.ScopedPublisher{Documents: documents, Tenant: d.request.TenantID, Run: plan.shell.Run}
			ticket, err = b.Reader.startNativeInputManualInitialPreparation(ctx, b.Scheduler, publisher, who, plan, capacity.Color)
		} else {
			ticket, err = b.Reader.restoreAndResumeNativeInputManualPreparation(ctx, b.Scheduler, who, producers, capacity.Color)
		}
		if err != nil {
			return zero, err
		}
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		if ticket.RunID != plan.shell.Run || ticket.WorkflowID != plan.shell.WorkflowID || ticket.State != "queued" {
			return zero, v2input.ErrConflict
		}
		return nativeInputManualInitialSubmission{plan: plan, ticket: ticket}, nil
	default:
		return zero, v2input.ErrConflict
	}
}
