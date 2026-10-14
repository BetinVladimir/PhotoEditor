package semanticproposal

import (
	"context"
	"errors"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type nativeFaceSubmission struct {
	plan   nativeFaceDraftPlan
	ticket v2input.JobTicket
}

// Starts an original landmark/parser child from the completed detector. The
// caller retains the original parent identity and must not expose this child
// receipt as a semantic parent receipt. Existing registered children resume via
// resumeNativeFacePreparation with their restored original plan.
func (b nativePreparationBootstrap) bootstrapFace(ctx context.Context, who auth.Identity, detector detectorDraftPlan, color TransferColorProfile, operation string, report v2input.Ref, budget nativePreparationBudget) (nativeFaceSubmission, error) {
	var zero nativeFaceSubmission
	if err := b.validate(ctx); err != nil {
		return zero, err
	}
	if operation != "face.landmarks" && operation != "face.parse" {
		return zero, v2input.ErrInvalid
	}
	if err := validateNativeFaceStartBudget(budget); err != nil {
		return zero, err
	}
	if err := verifyDetectorPlanSeal(ctx, detector); err != nil {
		return zero, err
	}
	owned, err := b.Reader.recompileDetectorDraftPlan(ctx, who, detector)
	if err != nil {
		return zero, err
	}
	if owned.seal != detector.seal {
		return zero, v2input.ErrConflict
	}
	published, err := b.Reader.publishNativeFaceStageParams(ctx, who, owned, color, b.Documents, operation)
	if err != nil {
		return zero, err
	}
	prepared, err := b.Reader.prepareNativeFaceTask(ctx, b.Catalog, who, owned, color, published, report, budget)
	if err != nil {
		return zero, err
	}
	plan, err := b.Reader.compileNativeFaceDraftPlan(ctx, who, owned, prepared, budget.Reserved)
	if err != nil {
		return zero, err
	}
	// Re-read physical detector content after compilation; transactional admission
	// repeats original detector/source/qualification locks independently.
	completed, err := b.Reader.readCompletedDetectorPreparation(ctx, who, owned, color)
	if err != nil {
		return zero, err
	}
	if completed.catalog != published.detector || completed.sourceBody != published.sourceBody || completed.body != published.facesBody || completed.detectorParamsBody != published.detectorParamsBody {
		return zero, v2input.ErrConflict
	}
	// An admitted immutable graph refuses blocked-draft persistence. Recovery
	// below requires complete saved transport and repeats all admission gates;
	// a conflict never authorizes rebuilding or repairing an existing graph.
	if _, err := b.Reader.persistNativeFacePreparationInputs(ctx, b.Scheduler, b.Transport, who, plan, color, completed); err != nil && !errors.Is(err, v2input.ErrConflict) {
		return zero, err
	}
	ticket, err := b.Reader.resumeNativeFacePreparation(ctx, b.Scheduler, who, plan, completed)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if ticket.RunID != plan.shell.Run || ticket.WorkflowID != plan.shell.WorkflowID || ticket.State != "queued" {
		return zero, v2input.ErrConflict
	}
	return nativeFaceSubmission{plan: plan, ticket: ticket}, nil
}

func validateNativeFaceStartBudget(b nativePreparationBudget) error {
	const max int64 = 9007199254740991
	const transport int64 = 8388608
	for _, n := range []int64{b.RAM, b.VRAM, b.Scratch, b.Output, b.Reserved} {
		if n < 1 || n > max {
			return v2input.ErrInvalid
		}
	}
	if b.Output > max-transport || b.Reserved < b.Output+transport {
		return v2input.ErrInvalid
	}
	return validateTransferMeasuredCosts(b.Costs, v2qualification.Metrics{})
}
