package semanticproposal

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Internal activation/recovery boundary. Unknown commit errors return no ticket;
// the same frozen request/plan may be retried through authenticated durable replay.
func activateNativeTransfer(ctx context.Context, scheduler *pgxpool.Pool, who auth.Identity, request Request, key string, published PublishedIntent, prepared PreparedTransferDraft) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	plan := prepared.plan
	if scheduler == nil || plan.tenant != request.TenantID || plan.actor != request.ActorID || who.UserID != request.ActorID || !plan.nativeReferenceRequired || prepared.RunID != request.RunID || prepared.WorkflowID != request.WorkflowID || plan.RunID() != request.RunID || plan.WorkflowID() != request.WorkflowID || plan.RequestSHA() != request.RequestSHA || plan.IntentRef() != published.Ref || !sameTransferInventory(plan.Inventory(), published.Inventory) {
		return zero, v2input.ErrInvalid
	}
	owned, err := CompileRequest(request.TenantID, who.UserID, key, request.Body)
	if err != nil {
		return zero, err
	}
	if owned.ActorID != request.ActorID || owned.RunID != request.RunID || owned.WorkflowID != request.WorkflowID || owned.RequestSHA != request.RequestSHA {
		return zero, v2input.ErrInvalid
	}
	var config v2input.SemanticDraftConfig
	if json.Unmarshal(plan.ConfigJSON(), &config) != nil || config.Semantic.AuthSessionID != who.SessionID {
		return zero, v2input.ErrInvalid
	}
	if _, err := BindWorkflowTransport(ctx, plan, prepared.transports); err != nil {
		return zero, err
	}
	if err := validateNativeTransferPlanScope(who, request, plan); err != nil {
		return zero, err
	}
	return nativeTicketWithRetry(ctx, scheduler.Begin, func(tx pgx.Tx) (v2input.JobTicket, error) {
		// Lock authority before observing the receipt. Concurrent admission cannot
		// seal between receipt selection and the original/replay branch.
		if err := lockWorkflowAdmissionEvidence(ctx, tx, who, request, published, plan); err != nil {
			return zero, err
		}
		var receipt []byte
		if err := tx.QueryRow(ctx, "SELECT photo.v2_admission_ticket($1,$2,$3,$4)", request.TenantID, who.UserID, key, request.RequestSHA).Scan(&receipt); err != nil {
			return zero, err
		}
		if len(receipt) != 0 {
			return replayNativeTransferInTransaction(ctx, tx, who, request, key, published, plan, prepared.transports)
		}
		return sealNativeTransferDraftInTransaction(ctx, tx, who, request, key, published, plan, prepared.transports)
	})
}

func nativeTicketWithRetry(ctx context.Context, begin func(context.Context) (pgx.Tx, error), work func(pgx.Tx) (v2input.JobTicket, error)) (v2input.JobTicket, error) {
	var ticket v2input.JobTicket
	err := retryProposalTransaction(ctx, begin, func(tx pgx.Tx) error {
		ticket = v2input.JobTicket{}
		result, err := work(tx)
		if err == nil {
			ticket = result
		}
		return err
	})
	if err != nil {
		return v2input.JobTicket{}, err
	}
	return ticket, nil
}
