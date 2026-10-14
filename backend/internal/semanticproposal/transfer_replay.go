package semanticproposal

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// replayPreparedTransfer is read-only with respect to admission. It repeats live
// authority and exact sealed transport closure before returning the original
// receipt. The historical completed-state SQL gate also supports ordinary
// semantic.transfer plans; native continuation is explicitly refused here.
func (p TransferDraftPreparer) replayPreparedTransfer(ctx context.Context, who auth.Identity, request Request, key string, prepared PreparedTransferDraft) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	plan := prepared.plan
	owned, err := CompileRequest(request.TenantID, who.UserID, key, request.Body)
	if err != nil {
		return zero, err
	}
	if p.Scheduler == nil || owned.ActorID != request.ActorID || owned.RunID != request.RunID || owned.WorkflowID != request.WorkflowID || owned.RequestSHA != request.RequestSHA || prepared.RunID != request.RunID || prepared.WorkflowID != request.WorkflowID || plan.nativeReferenceRequired || plan.nativeReference != nil || plan.tenant != request.TenantID || plan.actor != who.UserID || plan.RequestSHA() != request.RequestSHA || plan.IntentRef() != prepared.published.Ref || !sameTransferInventory(plan.Inventory(), prepared.published.Inventory) {
		return zero, v2input.ErrInvalid
	}
	var config v2input.SemanticDraftConfig
	if json.Unmarshal(plan.ConfigJSON(), &config) != nil || config.Semantic.AuthSessionID != who.SessionID {
		return zero, v2input.ErrInvalid
	}
	graph, err := BindWorkflowTransport(ctx, plan, prepared.transports)
	if err != nil {
		return zero, err
	}
	return nativeTicketWithRetry(ctx, p.Scheduler.Begin, func(tx pgx.Tx) (v2input.JobTicket, error) {
		if err := lockWorkflowAdmissionEvidence(ctx, tx, who, request, prepared.published, plan); err != nil {
			return zero, err
		}
		for i, task := range graph {
			state, err := workflowInputState(ctx, tx, request, plan.tasks[i], task, &plan)
			if err != nil {
				return zero, err
			}
			if state != "replayed" {
				return zero, v2input.ErrConflict
			}
		}
		var raw []byte
		if err := tx.QueryRow(ctx, "SELECT photo.v2_admission_ticket($1,$2,$3,$4)", request.TenantID, who.UserID, key, request.RequestSHA).Scan(&raw); err != nil {
			return zero, err
		}
		var ticket v2input.JobTicket
		if json.Unmarshal(raw, &ticket) != nil || ticket.RunID != request.RunID || ticket.WorkflowID != request.WorkflowID || ticket.State != "queued" {
			return zero, v2input.ErrInvalid
		}
		return ticket, nil
	})
}
