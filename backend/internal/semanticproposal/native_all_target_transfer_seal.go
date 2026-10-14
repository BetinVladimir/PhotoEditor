package semanticproposal

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Initial unsealed-graph activation only. All errors require caller rollback;
// the exported result is returned only by the commit-aware wrapper below.
func (s SQLTransferSourceReader) sealNativeAllTargetTransferInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, request Request, key string, published PublishedIntent, plan nativeAllTargetTransferWorkflowPlan, configuration TransferPreparation, completed json.RawMessage, transports []v2input.Built) (v2input.JobTicket, error) {
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
	if err := s.registerNativeAllTargetTransferInTransaction(ctx, tx, who, owned, published, plan, configuration, completed, transports); err != nil {
		return zero, fmt.Errorf("native transfer seal registration: %w", err)
	}
	var sealed bool
	if err := tx.QueryRow(ctx, "SELECT photo.seal_v2_graph($1,$2)", owned.TenantID, owned.RunID).Scan(&sealed); err != nil {
		return zero, fmt.Errorf("native transfer graph seal: %w", err)
	}
	if !sealed {
		return zero, fmt.Errorf("native transfer graph refused seal: %w", v2input.ErrConflict)
	}
	var raw []byte
	if err := tx.QueryRow(ctx, "SELECT photo.v2_admission_ticket($1,$2,$3,$4)", owned.TenantID, who.UserID, key, owned.RequestSHA).Scan(&raw); err != nil {
		return zero, err
	}
	ticket, err := decodeNativeInputTransferTicket(raw, owned)
	if err != nil {
		return zero, fmt.Errorf("native transfer sealed ticket: %w", err)
	}
	return ticket, nil
}
