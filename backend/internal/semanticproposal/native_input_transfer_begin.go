package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Creates only blocked transfer tasks, after complete original-native and target
// evidence locks. Caller commits before publishing transport and rolls back all
// errors. Sealed replay is handled separately by authenticated recovery.
func (s SQLTransferSourceReader) beginNativeInputTransferInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, request Request, key string, published PublishedIntent, plan nativeInputTransferWorkflowPlan, configuration TransferPreparation, completed json.RawMessage, reservedBytes int64) (DraftAdmission, error) {
	var zero DraftAdmission
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if tx == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || who.UserID != request.ActorID {
		return zero, v2input.ErrInvalid
	}
	owned, err := CompileRequest(request.TenantID, who.UserID, key, request.Body)
	if err != nil {
		return zero, err
	}
	if owned.RunID != request.RunID || owned.WorkflowID != request.WorkflowID || owned.RequestSHA != request.RequestSHA {
		return zero, v2input.ErrConflict
	}
	if err := s.validateNativeInputTransferPlanScope(ctx, who, owned, plan, configuration); err != nil {
		return zero, err
	}
	if plan.core.intent != published.Ref || !sameTransferInventory(plan.core.inventory, published.Inventory) {
		return zero, v2input.ErrConflict
	}
	shell, err := plan.core.DraftShell(reservedBytes)
	if err != nil {
		return zero, err
	}
	if err := s.lockNativeInputTransferAdmissionEvidence(ctx, tx, who, owned, published, plan, configuration, completed); err != nil {
		return zero, err
	}
	outcome, run, err := v2input.BeginAdmissionInTransaction(ctx, tx, shell, key, owned.RequestSHA)
	if err != nil {
		return zero, err
	}
	return nativeInputTransferDraftOutcome(owned.RunID, run, outcome)
}

func nativeInputTransferDraftOutcome(expected, actual uuid.UUID, outcome string) (DraftAdmission, error) {
	var zero DraftAdmission
	if outcome == "expired" {
		return zero, v2input.ErrExpired
	}
	if expected == uuid.Nil || actual != expected {
		return zero, v2input.ErrInvalid
	}
	if outcome != "created" && outcome != "pending" {
		return zero, v2input.ErrConflict
	}
	return DraftAdmission{RunID: actual, Outcome: outcome}, nil
}
