package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Reads registered transfer transport only under complete original input locks.
// Missing/partial/stale state is refused; recovery never republishes or repairs.
func (s SQLTransferSourceReader) recoverNativeInputTransferInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, request Request, key string, published PublishedIntent, plan nativeInputTransferWorkflowPlan, configuration TransferPreparation, completed json.RawMessage) (v2input.JobTicket, error) {
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
	if owned.RunID != request.RunID || owned.WorkflowID != request.WorkflowID || owned.RequestSHA != request.RequestSHA || owned.ActorID != request.ActorID {
		return zero, v2input.ErrConflict
	}
	if err := s.validateNativeInputTransferPlanScope(ctx, who, request, plan, configuration); err != nil {
		return zero, err
	}
	if err := s.lockNativeInputTransferAdmissionEvidence(ctx, tx, who, request, published, plan, configuration, completed); err != nil {
		return zero, err
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return zero, err
	}
	if role != "photo_scheduler" {
		return zero, v2input.ErrInvalid
	}
	transports := make([]v2input.Built, len(plan.core.tasks))
	for i, task := range plan.core.tasks {
		var raw []byte
		if err := tx.QueryRow(ctx, "SELECT photo.semantic_native_input_transfer_registered_transport($1,$2,$3,$4,$5)", request.TenantID, request.RunID, task.ID, who.UserID, who.SessionID).Scan(&raw); err != nil {
			return zero, err
		}
		if len(raw) == 0 {
			return zero, v2input.ErrConflict
		}
		built, err := decodeNativeRecoveredTransport(raw, task.ID)
		if err != nil {
			return zero, err
		}
		if _, err := plan.core.frozen[i].BindPublishedTransport(ctx, built); err != nil {
			return zero, err
		}
		transports[i] = built
	}
	var receipt []byte
	if err := tx.QueryRow(ctx, "SELECT photo.v2_admission_ticket($1,$2,$3,$4)", request.TenantID, who.UserID, key, request.RequestSHA).Scan(&receipt); err != nil {
		return zero, err
	}
	if len(receipt) != 0 {
		return s.replayNativeInputTransferInTransaction(ctx, tx, who, request, key, published, plan, configuration, completed, transports)
	}
	return s.sealNativeInputTransferInTransaction(ctx, tx, who, request, key, published, plan, configuration, completed, transports)
}

// Reconstructing the plan itself remains a separate server-owned operation.
// This resumes saved registered transport; no publisher or draft creation exists.
func (s SQLTransferSourceReader) resumeNativeInputTransfer(ctx context.Context, scheduler *pgxpool.Pool, who auth.Identity, request Request, key string, published PublishedIntent, plan nativeInputTransferWorkflowPlan, configuration TransferPreparation, defectBuilt v2input.Built) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if scheduler == nil {
		return zero, v2input.ErrInvalid
	}
	configuration = ownTransferPreparation(configuration)
	if err := s.recheckNativeInputTransferPhysical(ctx, who, request, plan, configuration); err != nil {
		return zero, err
	}
	p := plan.reference.person.source.protection.published.preparation
	completed, err := s.marshalNativeDefectCompletedEvidence(ctx, who, p.plan, configuration.Color, p.result, defectBuilt)
	if err != nil {
		return zero, err
	}
	return nativeTicketWithRetry(ctx, scheduler.Begin, func(tx pgx.Tx) (v2input.JobTicket, error) {
		return s.recoverNativeInputTransferInTransaction(ctx, tx, who, request, key, published, plan, configuration, completed)
	})
}
