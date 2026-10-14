package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Initial unsealed-graph activation only. All errors require caller rollback;
// the exported result is returned only by the commit-aware wrapper below.
func (s SQLTransferSourceReader) sealNativeInputManualTransferInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, request Request, key string, published PublishedIntent, plan nativeInputManualTransferWorkflowPlan, configuration TransferPreparation, completed json.RawMessage, transports []v2input.Built) (v2input.JobTicket, error) {
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
	if err := s.registerNativeInputManualTransferInTransaction(ctx, tx, who, owned, published, plan, configuration, completed, transports); err != nil {
		return zero, err
	}
	var sealed bool
	if err := tx.QueryRow(ctx, "SELECT photo.seal_v2_graph($1,$2)", owned.TenantID, owned.RunID).Scan(&sealed); err != nil {
		return zero, err
	}
	if !sealed {
		return zero, v2input.ErrConflict
	}
	var raw []byte
	if err := tx.QueryRow(ctx, "SELECT photo.v2_admission_ticket($1,$2,$3,$4)", owned.TenantID, who.UserID, key, owned.RequestSHA).Scan(&raw); err != nil {
		return zero, err
	}
	return decodeNativeInputTransferTicket(raw, owned)
}

// Physical reference and completed producer evidence are reverified before SQL.
// Sealed replay repeats complete stored closure checks and performs no writes.
func (s SQLTransferSourceReader) activateNativeInputManualTransfer(ctx context.Context, scheduler *pgxpool.Pool, who auth.Identity, request Request, key string, published PublishedIntent, plan nativeInputManualTransferWorkflowPlan, configuration TransferPreparation, defectBuilt v2input.Built, transports []v2input.Built) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if scheduler == nil {
		return zero, v2input.ErrInvalid
	}
	configuration = ownTransferPreparation(configuration)
	if err := s.validateNativeInputManualTransferPlanScope(ctx, who, request, plan, configuration); err != nil {
		return zero, err
	}
	if _, err := BindWorkflowTransport(ctx, plan.core, transports); err != nil {
		return zero, err
	}
	if err := s.recheckNativeInputManualTransferPhysical(ctx, who, request, plan, configuration); err != nil {
		return zero, err
	}
	p := plan.reference.person.source.protection.published.preparation
	completed, err := s.marshalNativeInputManualInitialCompletedEvidence(ctx, who, p.plan, configuration.Color, p.result, defectBuilt)
	if err != nil {
		return zero, err
	}
	return nativeTicketWithRetry(ctx, scheduler.Begin, func(tx pgx.Tx) (v2input.JobTicket, error) {
		if err := s.lockNativeInputManualTransferAdmissionEvidence(ctx, tx, who, request, published, plan, configuration, completed); err != nil {
			return zero, err
		}
		var receipt []byte
		if err := tx.QueryRow(ctx, "SELECT photo.v2_admission_ticket($1,$2,$3,$4)", request.TenantID, who.UserID, key, request.RequestSHA).Scan(&receipt); err != nil {
			return zero, err
		}
		if len(receipt) != 0 {
			return s.replayNativeInputManualTransferInTransaction(ctx, tx, who, request, key, published, plan, configuration, completed, transports)
		}
		return s.sealNativeInputManualTransferInTransaction(ctx, tx, who, request, key, published, plan, configuration, completed, transports)
	})
}
