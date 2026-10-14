package semanticproposal

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Revalidates original authority and exact sealed inputs before returning the
// saved acceptance ticket. No registration, sealing or event emission occurs.
// Caller must roll back on every error; source evidence is prepared before TX.
func replayNativeTransferInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, request Request, key string, published PublishedIntent, plan WorkflowPlan, transports []v2input.Built) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if tx == nil || !plan.nativeReferenceRequired || plan.nativeReference == nil || plan.tenant != request.TenantID || plan.actor != request.ActorID || who.UserID != request.ActorID || plan.RunID() != request.RunID || plan.WorkflowID() != request.WorkflowID || plan.RequestSHA() != request.RequestSHA || plan.IntentRef() != published.Ref || !sameTransferInventory(plan.Inventory(), published.Inventory) {
		return zero, v2input.ErrInvalid
	}
	owned, err := CompileRequest(request.TenantID, who.UserID, key, request.Body)
	if err != nil {
		return zero, err
	}
	if owned.RunID != request.RunID || owned.WorkflowID != request.WorkflowID || owned.RequestSHA != request.RequestSHA {
		return zero, v2input.ErrInvalid
	}
	var config v2input.SemanticDraftConfig
	if json.Unmarshal(plan.ConfigJSON(), &config) != nil || config.Semantic.AuthSessionID != who.SessionID {
		return zero, v2input.ErrInvalid
	}
	graph, err := BindWorkflowTransport(ctx, plan, transports)
	if err != nil {
		return zero, err
	}
	if err := lockWorkflowAdmissionEvidence(ctx, tx, who, request, published, plan); err != nil {
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
		return zero, v2input.ErrConflict
	}
	return ticket, nil
}
