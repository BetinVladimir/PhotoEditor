package semanticproposal

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

// Commits blocked metadata before external transport publication. Completed
// defect evidence is reread outside each SQL phase. No task is activated here.
func (s SQLTransferSourceReader) persistNativeInputManualTransfer(ctx context.Context, scheduler *pgxpool.Pool, publisher v2input.Publisher, who auth.Identity, request Request, key string, published PublishedIntent, plan nativeInputManualTransferWorkflowPlan, configuration TransferPreparation, defectBuilt v2input.Built, reservedBytes int64) ([]v2input.PreparedSemanticTask, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if scheduler == nil || publisher == nil {
		return nil, v2input.ErrInvalid
	}
	configuration = ownTransferPreparation(configuration)
	if err := s.validateNativeInputManualTransferPlanScope(ctx, who, request, plan, configuration); err != nil {
		return nil, err
	}
	p := plan.reference.person.source.protection.published.preparation
	completed, err := s.marshalNativeInputManualInitialCompletedEvidence(ctx, who, p.plan, configuration.Color, p.result, defectBuilt)
	if err != nil {
		return nil, err
	}
	return persistNativeInputTransferStages(ctx,
		func(callback func(pgx.Tx) error) error { return database.InTransaction(ctx, scheduler, callback) },
		func(tx pgx.Tx) error {
			_, err := s.beginNativeInputManualTransferInTransaction(ctx, tx, who, request, key, published, plan, configuration, completed, reservedBytes)
			return err
		},
		func() ([]v2input.PreparedSemanticTask, error) {
			prepared, err := PublishWorkflowTasks(ctx, plan.core, publisher)
			if err != nil {
				return nil, err
			}
			if err := s.recheckNativeInputManualTransferPhysical(ctx, who, request, plan, configuration); err != nil {
				return nil, err
			}
			completed, err = s.marshalNativeInputManualInitialCompletedEvidence(ctx, who, p.plan, configuration.Color, p.result, defectBuilt)
			if err != nil {
				return nil, err
			}
			return prepared, nil
		},
		func(tx pgx.Tx, prepared []v2input.PreparedSemanticTask) error {
			built := make([]v2input.Built, len(prepared))
			for i, task := range prepared {
				built[i] = task.Graph.Built
			}
			return s.registerNativeInputManualTransferInTransaction(ctx, tx, who, request, published, plan, configuration, completed, built)
		})
}
