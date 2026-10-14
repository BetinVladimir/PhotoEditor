package semanticproposal

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// RegisterWorkflowInputs commits all task input registrations together. Unknown
// commit outcomes are recovered by replaying the same frozen plan/transports.
func RegisterWorkflowInputs(ctx context.Context, scheduler *pgxpool.Pool, who auth.Identity, request Request, published PublishedIntent, plan WorkflowPlan, transports []v2input.Built) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if scheduler == nil {
		return v2input.ErrInvalid
	}
	return retryProposalTransaction(ctx, scheduler.Begin, func(tx pgx.Tx) error {
		return RegisterWorkflowInputsInTransaction(ctx, tx, who, request, published, plan, transports)
	})
}
