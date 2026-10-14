package workflow

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// ActivateInTransaction registers the pinned closure then atomically seals and
// links the child under its expansion fence. Caller must roll back on every
// error; no object IO or independent commit occurs here.
func (f FrozenDeliveryInitialTask) ActivateInTransaction(ctx context.Context, tx pgx.Tx, lease delivery.ExpansionLease, plan delivery.PreparePlan, child delivery.ExportChildIntent, exportID uuid.UUID, graph v2input.GraphTask) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	draft, err := f.DraftTask()
	if err != nil {
		return err
	}
	if tx == nil || graph.ID != draft.ID || exportID == uuid.Nil || lease.ParentTask != plan.TaskID() || child.SlotIndex > lease.Cursor {
		return v2input.ErrInvalid
	}
	workflow, err := v2input.WorkflowID(child.RunID)
	if err != nil {
		return err
	}
	task, err := v2input.TaskID(workflow, draft.Operation, 0)
	if err != nil || task != draft.ID {
		return v2input.ErrInvalid
	}
	fresh, err := (delivery.ExpansionLeaseStore{DB: tx}).LoadPlan(ctx, lease)
	if err != nil {
		return err
	}
	if !bytes.Equal(fresh.ProofJCS(), plan.ProofJCS()) {
		return v2input.ErrInvalid
	}
	_, request, err := fresh.ExportRequest(lease.Run, child)
	if err != nil {
		return err
	}
	// Use the owned cursor/direct refs, never caller-replaced GraphTask.Open.
	if err := v2input.RegisterInTransaction(ctx, tx, lease.Tenant, child.RunID, draft.ID, graph.Built, f.closure.Open, f.closure.Direct); err != nil {
		return err
	}
	var accepted bool
	err = tx.QueryRow(ctx, `SELECT photo.activate_v30_delivery_child($1,$2,$3,$4,$5)`, lease.Expansion, lease.Token, child.SlotIndex, string(request), exportID).Scan(&accepted)
	if err != nil {
		return err
	}
	if !accepted {
		return domain.NewError("DELIVERY_CHILD_ACTIVATION_REFUSED", 409)
	}
	return nil
}
