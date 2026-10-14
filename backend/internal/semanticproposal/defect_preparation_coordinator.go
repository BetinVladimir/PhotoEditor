package semanticproposal

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

// Persists a blocked preparation draft, publishes its frozen input transport
// outside SQL, then durably registers the exact closure. Failure leaves a
// recoverable draft; retries use the same deterministic IDs and document slots.
// Final policy admission and queue activation remain separate.
func (s SQLTransferSourceReader) persistDefectPreparationInputs(ctx context.Context, scheduler *pgxpool.Pool, publisher v2input.Publisher, who auth.Identity, plan defectPreparationDraftPlan) (v2input.PreparedSemanticTask, error) {
	var zero v2input.PreparedSemanticTask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if scheduler == nil || publisher == nil {
		return zero, v2input.ErrInvalid
	}
	if err := database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
		_, err := s.beginDefectPreparationDraftInTransaction(ctx, tx, who, plan)
		return err
	}); err != nil {
		return zero, fmt.Errorf("persist defect preparation draft: %w", err)
	}
	prepared, err := v2input.PublishSemanticDefectTask(ctx, plan.frozen, publisher)
	if err != nil {
		return zero, fmt.Errorf("publish defect input closure: %w", err)
	}
	if err := database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
		return s.registerDefectPreparationInputsInTransaction(ctx, tx, who, plan, prepared.Graph.Built)
	}); err != nil {
		return zero, fmt.Errorf("register defect input closure: %w", err)
	}
	return prepared, nil
}

// Commits only SQL admission after immutable input publication has finished.
// The complete callback is retryable: it performs no object-store or queue IO.
// A failed callback or commit never returns a usable ticket to the caller.
func (s SQLTransferSourceReader) sealRegisteredDefectPreparation(ctx context.Context, scheduler *pgxpool.Pool, who auth.Identity, prepared defectPersonAdmissionPreparation, built v2input.Built) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if scheduler == nil {
		return zero, v2input.ErrInvalid
	}
	return nativeTicketWithRetry(ctx, scheduler.Begin, func(tx pgx.Tx) (v2input.JobTicket, error) {
		return s.sealDefectPreparationInTransaction(ctx, tx, who, prepared, built)
	})
}
