package semanticproposal

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Physical reference and completed producer evidence are reverified before SQL.
// Sealed replay repeats complete stored closure checks and performs no writes.
func (s SQLTransferSourceReader) activateNativeAllTargetTransfer(ctx context.Context, scheduler *pgxpool.Pool, who auth.Identity, request Request, key string, published PublishedIntent, plan nativeAllTargetTransferWorkflowPlan, configuration TransferPreparation, defectBuilt v2input.Built, transports []v2input.Built) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if scheduler == nil {
		return zero, v2input.ErrInvalid
	}
	configuration = ownTransferPreparation(configuration)
	if err := s.validateNativeAllTargetTransferPlanScope(ctx, who, request, plan, configuration); err != nil {
		return zero, fmt.Errorf("native transfer activation plan scope: %w", err)
	}
	if _, err := BindWorkflowTransport(ctx, plan.core, transports); err != nil {
		return zero, fmt.Errorf("native transfer activation transport: %w", err)
	}
	if err := s.recheckNativeAllTargetTransferPhysical(ctx, who, request, plan, configuration); err != nil {
		return zero, fmt.Errorf("native transfer activation physical inputs: %w", err)
	}
	p := plan.reference.person.source.protection.published.preparation
	completed, err := s.marshalNativeDefectCompletedEvidence(ctx, who, p.plan, configuration.Color, p.result, defectBuilt)
	if err != nil {
		return zero, err
	}
	return nativeTicketWithRetry(ctx, scheduler.Begin, func(tx pgx.Tx) (v2input.JobTicket, error) {
		if err := s.lockNativeAllTargetTransferAdmissionEvidence(ctx, tx, who, request, published, plan, configuration, completed); err != nil {
			return zero, fmt.Errorf("native transfer activation admission locks: %w", err)
		}
		var receipt []byte
		if err := tx.QueryRow(ctx, "SELECT photo.v2_admission_ticket($1,$2,$3,$4)", request.TenantID, who.UserID, key, request.RequestSHA).Scan(&receipt); err != nil {
			return zero, err
		}
		if len(receipt) != 0 {
			return s.replayNativeAllTargetTransferInTransaction(ctx, tx, who, request, key, published, plan, configuration, completed, transports)
		}
		return s.sealNativeAllTargetTransferInTransaction(ctx, tx, who, request, key, published, plan, configuration, completed, transports)
	})
}
