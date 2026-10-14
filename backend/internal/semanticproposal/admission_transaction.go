package semanticproposal

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// BeginDraft commits the blocked draft and its idempotency binding together.
// The pool must authenticate as photo_scheduler; the transactional locks verify
// that role. It performs no object IO and never activates or seals the graph.
func BeginDraft(ctx context.Context, scheduler *pgxpool.Pool, who auth.Identity, request Request, key string, published PublishedIntent, plan WorkflowPlan, reservedBytes int64) (DraftAdmission, error) {
	if err := ctx.Err(); err != nil {
		return DraftAdmission{}, err
	}
	if scheduler == nil {
		return DraftAdmission{}, v2input.ErrInvalid
	}
	return beginDraftWithRetry(ctx, scheduler.Begin, func(tx pgx.Tx) (DraftAdmission, error) {
		return BeginDraftInTransaction(ctx, tx, who, request, key, published, plan, reservedBytes)
	})
}

// Results from an aborted or unknown commit must never escape as an admission.
func beginDraftWithRetry(ctx context.Context, begin func(context.Context) (pgx.Tx, error), work func(pgx.Tx) (DraftAdmission, error)) (DraftAdmission, error) {
	var admission DraftAdmission
	err := retryProposalTransaction(ctx, begin, func(tx pgx.Tx) error {
		admission = DraftAdmission{}
		result, err := work(tx)
		if err == nil {
			admission = result
		}
		return err
	})
	if err != nil {
		return DraftAdmission{}, err
	}
	return admission, nil
}
