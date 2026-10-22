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
func (s SQLTransferSourceReader) persistNativeAllTargetTransfer(ctx context.Context, scheduler *pgxpool.Pool, publisher v2input.Publisher, who auth.Identity, request Request, key string, published PublishedIntent, plan nativeAllTargetTransferWorkflowPlan, configuration TransferPreparation, defectBuilt v2input.Built, reservedBytes int64) ([]v2input.PreparedSemanticTask, error) {
	if scheduler == nil {
		return nil, v2input.ErrInvalid
	}
	return s.persistNativeAllTargetTransferWithTransaction(ctx, func(callback func(pgx.Tx) error) error {
		return database.InTransaction(ctx, scheduler, callback)
	}, publisher, who, request, key, published, plan, configuration, defectBuilt, reservedBytes)
}

// The transaction runner owns commit acknowledgement; only committed stages
// may expose prepared transports. External publication stays outside it.
func (s SQLTransferSourceReader) persistNativeAllTargetTransferWithTransaction(ctx context.Context, transaction func(func(pgx.Tx) error) error, publisher v2input.Publisher, who auth.Identity, request Request, key string, published PublishedIntent, plan nativeAllTargetTransferWorkflowPlan, configuration TransferPreparation, defectBuilt v2input.Built, reservedBytes int64) ([]v2input.PreparedSemanticTask, error) {
	if err := ctx.Err(); err != nil {
		return nil, nativeAllTargetAdmissionError("context", err)
	}
	if transaction == nil || publisher == nil {
		return nil, v2input.ErrInvalid
	}
	configuration = ownTransferPreparation(configuration)
	if err := s.validateNativeAllTargetTransferPlanScope(ctx, who, request, plan, configuration); err != nil {
		return nil, nativeAllTargetAdmissionError("plan_scope", err)
	}
	if err := s.recheckNativeAllTargetTransferPhysical(ctx, who, request, plan, configuration); err != nil {
		return nil, nativeAllTargetAdmissionError("physical_before", err)
	}
	p := plan.reference.person.source.protection.published.preparation
	completed, err := s.marshalNativeDefectCompletedEvidence(ctx, who, p.plan, configuration.Color, p.result, defectBuilt)
	if err != nil {
		return nil, nativeAllTargetAdmissionError("completed_before", err)
	}
	return persistNativeInputTransferStages(ctx,
		transaction,
		func(tx pgx.Tx) error {
			_, err := s.beginNativeAllTargetTransferInTransaction(ctx, tx, who, request, key, published, plan, configuration, completed, reservedBytes)
			return nativeAllTargetAdmissionError("begin_draft", err)
		},
		func() ([]v2input.PreparedSemanticTask, error) {
			prepared, err := PublishWorkflowTasks(ctx, plan.core, publisher)
			if err != nil {
				return nil, nativeAllTargetAdmissionError("publish_workflow", err)
			}
			if err := s.recheckNativeAllTargetTransferPhysical(ctx, who, request, plan, configuration); err != nil {
				return nil, nativeAllTargetAdmissionError("physical_after", err)
			}
			completed, err = s.marshalNativeDefectCompletedEvidence(ctx, who, p.plan, configuration.Color, p.result, defectBuilt)
			if err != nil {
				return nil, nativeAllTargetAdmissionError("completed_after", err)
			}
			return prepared, nil
		},
		func(tx pgx.Tx, prepared []v2input.PreparedSemanticTask) error {
			built := make([]v2input.Built, len(prepared))
			for i, task := range prepared {
				built[i] = task.Graph.Built
			}
			return nativeAllTargetAdmissionError("register_inputs", s.registerNativeAllTargetTransferInTransaction(ctx, tx, who, request, published, plan, configuration, completed, built))
		})
}
