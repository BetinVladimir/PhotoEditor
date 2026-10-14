package semanticproposal

import (
	"context"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// admitPreparedTransfer performs no object IO. Preparation must have verified
// the full ordinary source/policy closure. A ticket escapes only after commit;
// unknown commit outcomes expose no ticket and require durable recovery.
func (p TransferDraftPreparer) admitPreparedTransfer(ctx context.Context, who auth.Identity, request Request, key string, prepared PreparedTransferDraft) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if p.Scheduler == nil || prepared.RunID != request.RunID || prepared.WorkflowID != request.WorkflowID || (prepared.Outcome != "created" && prepared.Outcome != "pending") || prepared.plan.nativeReferenceRequired || prepared.plan.nativeReference != nil {
		return zero, v2input.ErrInvalid
	}
	return nativeTicketWithRetry(ctx, p.Scheduler.Begin, func(tx pgx.Tx) (v2input.JobTicket, error) {
		return sealTransferDraftInTransaction(ctx, tx, who, request, key, prepared.published, prepared.plan, prepared.transports)
	})
}
