package semanticproposal

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// sealTransferDraftInTransaction activates only an unsealed ordinary transfer draft.
// Caller rolls back every error, including errors after graph activation. All
// object verification/publication must precede this DB-only transaction.
// Native continuation must use its dedicated native admission boundary.
// Completed ticket replay is a separate authenticated recovery operation.
func sealTransferDraftInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, request Request, key string, published PublishedIntent, plan WorkflowPlan, transports []v2input.Built) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if tx == nil || plan.nativeReferenceRequired || plan.nativeReference != nil {
		return zero, v2input.ErrInvalid
	}
	owned, err := CompileRequest(request.TenantID, who.UserID, key, request.Body)
	if err != nil {
		return zero, err
	}
	if owned.ActorID != request.ActorID || owned.RunID != request.RunID || owned.WorkflowID != request.WorkflowID || owned.RequestSHA != request.RequestSHA {
		return zero, v2input.ErrInvalid
	}
	// This repeats all source, inventory, snapshot, qualification, protection,
	// person and exact registered transport locks in the activation transaction.
	if err := RegisterWorkflowInputsInTransaction(ctx, tx, who, request, published, plan, transports); err != nil {
		return zero, fmt.Errorf("ordinary transfer input authority: %w", err)
	}
	var sealed bool
	if err := tx.QueryRow(ctx, "SELECT photo.seal_v2_graph($1,$2)", request.TenantID, request.RunID).Scan(&sealed); err != nil {
		return zero, err
	}
	if !sealed {
		return zero, fmt.Errorf("ordinary transfer graph seal refused: %w", v2input.ErrConflict)
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
}
