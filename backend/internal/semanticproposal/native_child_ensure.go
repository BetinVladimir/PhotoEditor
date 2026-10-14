package semanticproposal

import (
	"context"
	"errors"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// The durable caller holds the original parent lock and supplies a publisher
// scoped to the deterministic child run. Absence is checked by the authenticated
// detector gate, not inferred from a failed restore. Concurrent creation still
// passes the original begin/register/seal gates; failures expose no submission.
func (b nativePreparationBootstrap) ensureFace(ctx context.Context, who auth.Identity, detector detectorDraftPlan, color TransferColorProfile, operation string, report v2input.Ref, budget nativePreparationBudget) (nativeFaceSubmission, error) {
	var zero nativeFaceSubmission
	if err := b.validate(ctx); err != nil {
		return zero, err
	}
	presence, err := b.Reader.readNativeStagePresence(ctx, who, detector, operation)
	if err != nil {
		return zero, err
	}
	switch presence {
	case nativeStageAbsent:
		return b.bootstrapFace(ctx, who, detector, color, operation, report, budget)
	case nativeStagePresent:
		plan, err := b.Reader.restoreNativeFaceDraftPlan(ctx, b.Catalog, who, detector, color, operation)
		if err != nil {
			return zero, err
		}
		completed, err := b.Reader.readCompletedDetectorPreparation(ctx, who, detector, color)
		if err != nil {
			return zero, err
		}
		// An interrupted initial attempt may have committed the blocked graph
		// before publishing/registering transport. Repeat immutable publication
		// from the exact restored plan, never from new report/resource inputs.
		// Admitted graphs refuse blocked-draft persistence. Only complete
		// saved transport can pass the independent recovery/seal gate below.
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
	default:
		return zero, v2input.ErrConflict
	}
}

func (b nativePreparationBootstrap) ensureSkin(ctx context.Context, who auth.Identity, parser nativeFaceDraftPlan, color TransferColorProfile, budget nativePreparationBudget) (nativeSkinSubmission, error) {
	var zero nativeSkinSubmission
	if err := b.validate(ctx); err != nil {
		return zero, err
	}
	if err := verifyNativeFacePlanSeal(ctx, parser); err != nil {
		return zero, err
	}
	if parser.prepared.operation != "face.parse" {
		return zero, v2input.ErrInvalid
	}
	presence, err := b.Reader.readNativeStagePresence(ctx, who, parser.detector, "mask.face_skin")
	if err != nil {
		return zero, err
	}
	var submission nativeSkinSubmission
	switch presence {
	case nativeStageAbsent:
		submission, err = b.Reader.startNativeSkinPreparation(ctx, b.Scheduler, b.Documents, b.Transport, who, parser, color, budget.RAM, budget.Scratch, budget.Output, budget.Reserved)
	case nativeStagePresent:
		plan, restoreErr := b.Reader.restoreNativeSkinDraftPlan(ctx, who, parser, color)
		if restoreErr != nil {
			return zero, restoreErr
		}
		// A saved admitted child refuses blocked-draft persistence. The full
		// transport recovery/seal below independently rechecks original evidence;
		// conflict cannot authorize repair, publication or a fabricated receipt.
		if _, err := b.Reader.persistNativeSkinPreparationInputs(ctx, b.Scheduler, b.Transport, who, plan, color); err != nil && !errors.Is(err, v2input.ErrConflict) {
			return zero, err
		}
		ticket, resumeErr := b.Reader.resumeNativeSkinPreparation(ctx, b.Scheduler, who, plan)
		if resumeErr != nil {
			return zero, resumeErr
		}
		submission = nativeSkinSubmission{plan: plan, ticket: ticket}
	default:
		return zero, v2input.ErrConflict
	}
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if submission.ticket.RunID != submission.plan.shell.Run || submission.ticket.WorkflowID != submission.plan.shell.WorkflowID || submission.ticket.State != "queued" {
		return zero, v2input.ErrConflict
	}
	return submission, nil
}
