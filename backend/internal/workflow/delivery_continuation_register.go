package workflow

import (
	"context"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// CreateInTransaction registers a blocked continuation and its additional output
// reservation. Caller owns rollback/commit; transport publication follows commit.
func (f FrozenDeliveryContinuationTask) CreateInTransaction(ctx context.Context, tx delivery.ProofTransaction, lease delivery.ExpansionLease, plan delivery.PreparePlan, child delivery.ExportChildIntent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || lease.ParentTask != plan.TaskID() || f.stage < 1 || f.stage > 2 {
		return v2input.ErrInvalid
	}
	if _, _, err := plan.ExportRequest(lease.Run, child); err != nil {
		return err
	}
	draft, err := f.DraftTask()
	if err != nil {
		return err
	}
	workflow, err := v2input.WorkflowID(child.RunID)
	if err != nil {
		return err
	}
	id, err := v2input.TaskID(workflow, draft.Operation, 0)
	if err != nil || id != draft.ID {
		return v2input.ErrInvalid
	}
	estimate, err := ParseResourceEstimate(draft.ExecutionProfile)
	if err != nil || estimate.OutputBytes < 1 {
		return v2input.ErrInvalid
	}
	task, err := canonical.Marshal(draft)
	if err != nil {
		return err
	}
	var accepted bool
	err = tx.QueryRow(ctx, `SELECT photo.create_v30_delivery_continuation_task($1,$2,$3,$4,$5,$6::jsonb,$7,$8)`, lease.Expansion, lease.Token, child.SlotIndex, f.stage, f.predecessor, string(task), string(f.task.closure.ParamsJCS), estimate.OutputBytes).Scan(&accepted)
	if err != nil {
		return err
	}
	if !accepted {
		return domain.NewError("DELIVERY_CONTINUATION_TASK_REFUSED", 409)
	}
	return nil
}
