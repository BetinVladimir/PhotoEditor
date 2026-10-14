package workflow

import (
	"context"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
)

// RegisterInputsInTransaction attaches the previously published transport to a
// blocked continuation. It rechecks descriptor/Params/predecessor/lease via the
// idempotent SQL creation predicate, then registers only the owned frozen cursor.
// Caller must roll back every error. This neither seals the run nor dispatches
// the task. Blocked registered retries use the dedicated receipt; dispatched
// recovery belongs to the future activation receipt.
func (f FrozenDeliveryContinuationTask) RegisterInputsInTransaction(ctx context.Context, tx pgx.Tx, lease delivery.ExpansionLease, plan delivery.PreparePlan, child delivery.ExportChildIntent, graph v2input.GraphTask) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	draft, err := f.DraftTask()
	if err != nil {
		return err
	}
	if graph.ID != draft.ID {
		return v2input.ErrInvalid
	}
	registered, err := f.TransportRegistered(ctx, tx, lease, plan, child)
	if err != nil || registered {
		return err
	}
	if err := f.CreateInTransaction(ctx, tx, lease, plan, child); err != nil {
		return err
	}
	return v2input.RegisterInTransaction(ctx, tx, lease.Tenant, child.RunID, draft.ID, graph.Built, f.task.closure.Open, f.task.closure.Direct)
}
