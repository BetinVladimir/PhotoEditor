package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Sealed replay repeats complete live authority and exact stored task/transport
// state. It does not register inputs, reseal, emit events or republish objects.
func (s SQLTransferSourceReader) replayNativeInputTransferInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, request Request, key string, published PublishedIntent, plan nativeInputTransferWorkflowPlan, configuration TransferPreparation, completed json.RawMessage, transports []v2input.Built) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	owned, err := CompileRequest(request.TenantID, who.UserID, key, request.Body)
	if err != nil {
		return zero, err
	}
	if owned.ActorID != request.ActorID || owned.RunID != request.RunID || owned.WorkflowID != request.WorkflowID || owned.RequestSHA != request.RequestSHA {
		return zero, v2input.ErrConflict
	}
	if err := s.validateNativeInputTransferPlanScope(ctx, who, owned, plan, configuration); err != nil {
		return zero, err
	}
	if plan.core.intent != published.Ref || !sameTransferInventory(plan.core.inventory, published.Inventory) {
		return zero, v2input.ErrConflict
	}
	graph, err := BindWorkflowTransport(ctx, plan.core, transports)
	if err != nil {
		return zero, err
	}
	if err := s.lockNativeInputTransferAdmissionEvidence(ctx, tx, who, owned, published, plan, configuration, completed); err != nil {
		return zero, err
	}
	for i, task := range graph {
		state, err := workflowInputState(ctx, tx, owned, plan.core.tasks[i], task, &plan.core)
		if err != nil {
			return zero, err
		}
		if state != "replayed" {
			return zero, v2input.ErrConflict
		}
	}
	var raw []byte
	if err := tx.QueryRow(ctx, "SELECT photo.v2_admission_ticket($1,$2,$3,$4)", owned.TenantID, who.UserID, key, owned.RequestSHA).Scan(&raw); err != nil {
		return zero, err
	}
	return decodeNativeInputTransferTicket(raw, owned)
}
