package semanticproposal

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type nativeSkinSubmission struct {
	plan   nativeSkinDraftPlan
	ticket v2input.JobTicket
}

// Starts the CPU skin child from an owned, completed parser plan. Each phase
// repeats current authority; object IO stays outside scheduler transactions.
// Errors expose no plan or ticket, including unknown commit outcomes. Existing
// registered submissions use resumeNativeSkinPreparation instead of this path.
func (s SQLTransferSourceReader) startNativeSkinPreparation(ctx context.Context, scheduler *pgxpool.Pool, documents v2draft.Publisher, transport v2input.Publisher, who auth.Identity, parser nativeFaceDraftPlan, color TransferColorProfile, ram, scratch, output, reserved int64) (nativeSkinSubmission, error) {
	var zero nativeSkinSubmission
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if scheduler == nil || transport == nil || ram < 1 || scratch < 1 || output < 1 || output > 9007199254740991-8388608 || reserved < output+8388608 || reserved > 9007199254740991 {
		return zero, v2input.ErrInvalid
	}
	if err := verifyNativeFacePlanSeal(ctx, parser); err != nil {
		return zero, err
	}
	owned, err := s.compileNativeFaceDraftPlan(ctx, who, parser.detector, parser.prepared, parser.shell.Bytes)
	if err != nil {
		return zero, err
	}
	published, err := s.publishNativeSkinStageParams(ctx, who, owned, color, documents)
	if err != nil {
		return zero, err
	}
	prepared, err := s.prepareNativeSkinTask(ctx, who, owned, color, published, ram, scratch, output)
	if err != nil {
		return zero, err
	}
	plan, err := s.compileNativeSkinDraftPlan(ctx, who, owned, prepared, reserved)
	if err != nil {
		return zero, err
	}
	if _, err := s.persistNativeSkinPreparationInputs(ctx, scheduler, transport, who, plan, color); err != nil {
		return zero, err
	}
	ticket, err := s.resumeNativeSkinPreparation(ctx, scheduler, who, plan)
	if err != nil {
		return zero, err
	}
	return nativeSkinSubmission{plan: plan, ticket: ticket}, nil
}
