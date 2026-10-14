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
func (s SQLTransferSourceReader) persistNativeInputTransfer(ctx context.Context, scheduler *pgxpool.Pool, publisher v2input.Publisher, who auth.Identity, request Request, key string, published PublishedIntent, plan nativeInputTransferWorkflowPlan, configuration TransferPreparation, defectBuilt v2input.Built, reservedBytes int64) ([]v2input.PreparedSemanticTask, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if scheduler == nil || publisher == nil {
		return nil, v2input.ErrInvalid
	}
	configuration = ownTransferPreparation(configuration)
	if err := s.validateNativeInputTransferPlanScope(ctx, who, request, plan, configuration); err != nil {
		return nil, err
	}
	p := plan.reference.person.source.protection.published.preparation
	completed, err := s.marshalNativeDefectCompletedEvidence(ctx, who, p.plan, configuration.Color, p.result, defectBuilt)
	if err != nil {
		return nil, err
	}
	return persistNativeInputTransferStages(ctx,
		func(callback func(pgx.Tx) error) error { return database.InTransaction(ctx, scheduler, callback) },
		func(tx pgx.Tx) error {
			_, err := s.beginNativeInputTransferInTransaction(ctx, tx, who, request, key, published, plan, configuration, completed, reservedBytes)
			return err
		},
		func() ([]v2input.PreparedSemanticTask, error) {
			prepared, err := PublishWorkflowTasks(ctx, plan.core, publisher)
			if err != nil {
				return nil, err
			}
			if err := s.recheckNativeInputTransferPhysical(ctx, who, request, plan, configuration); err != nil {
				return nil, err
			}
			completed, err = s.marshalNativeDefectCompletedEvidence(ctx, who, p.plan, configuration.Color, p.result, defectBuilt)
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
			return s.registerNativeInputTransferInTransaction(ctx, tx, who, request, published, plan, configuration, completed, built)
		})
}

// A transaction callback success is insufficient: only committed stages expose
// a result. Publication is never retried as part of a SQL transaction retry.
func persistNativeInputTransferStages(ctx context.Context, transaction func(func(pgx.Tx) error) error, begin func(pgx.Tx) error, publish func() ([]v2input.PreparedSemanticTask, error), register func(pgx.Tx, []v2input.PreparedSemanticTask) error) ([]v2input.PreparedSemanticTask, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if transaction == nil || begin == nil || publish == nil || register == nil {
		return nil, v2input.ErrInvalid
	}
	if err := transaction(begin); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	prepared, err := publish()
	if err != nil {
		return nil, err
	}
	if len(prepared) == 0 {
		return nil, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := transaction(func(tx pgx.Tx) error { return register(tx, prepared) }); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return prepared, nil
}
