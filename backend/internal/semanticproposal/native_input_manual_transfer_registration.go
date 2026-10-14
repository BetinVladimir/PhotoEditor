package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Registers transport only after all independently prepared native-input and
// target authorities are locked. Caller rolls back every error, including a
// refusal after a prior target registration. This never activates the graph.
func (s SQLTransferSourceReader) registerNativeInputManualTransferInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, request Request, published PublishedIntent, plan nativeInputManualTransferWorkflowPlan, configuration TransferPreparation, completed json.RawMessage, transports []v2input.Built) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	if err := s.validateNativeInputManualTransferPlanScope(ctx, who, request, plan, configuration); err != nil {
		return err
	}
	core := plan.core
	if core.intent != published.Ref || !sameTransferInventory(core.inventory, published.Inventory) {
		return v2input.ErrConflict
	}
	graph, err := BindWorkflowTransport(ctx, core, transports)
	if err != nil {
		return err
	}
	if err := s.lockNativeInputManualTransferAdmissionEvidence(ctx, tx, who, request, published, plan, configuration, completed); err != nil {
		return err
	}
	for i, task := range graph {
		state, err := registrationState(ctx, tx, request, core.tasks[i], task)
		if err != nil {
			return err
		}
		if state == "replayed" {
			continue
		}
		if state != "missing" {
			return v2input.ErrConflict
		}
		if err := v2input.RegisterInTransaction(ctx, tx, request.TenantID, request.RunID, task.ID, task.Built, task.Open, task.Direct); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (s SQLTransferSourceReader) lockNativeInputManualTransferAdmissionEvidence(ctx context.Context, tx pgx.Tx, who auth.Identity, request Request, published PublishedIntent, plan nativeInputManualTransferWorkflowPlan, configuration TransferPreparation, completed json.RawMessage) error {
	if err := s.lockNativeInputManualTransferPreparedInputs(ctx, tx, who, request, plan, configuration, completed); err != nil {
		return err
	}
	if err := LockPublishedIntentInTransaction(ctx, tx, who, request, published); err != nil {
		return err
	}
	if err := LockSnapshotInTransaction(ctx, tx, who, request, plan.core); err != nil {
		return err
	}
	if err := LockQualificationInTransaction(ctx, tx, who, request, plan.core); err != nil {
		return err
	}
	// Repeat complete prepared inputs after subsequent locks, as for the existing
	// transfer route. All object IO is finished before this caller transaction.
	return s.lockNativeInputManualTransferPreparedInputs(ctx, tx, who, request, plan, configuration, completed)
}
