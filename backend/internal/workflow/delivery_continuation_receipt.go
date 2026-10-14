package workflow

import (
	"context"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
)

// TransportRegistered acknowledges only the current blocked task's complete
// persisted input transport. false includes stale authority, not just absence.
func (f FrozenDeliveryContinuationTask) TransportRegistered(ctx context.Context, db delivery.ProofTransaction, lease delivery.ExpansionLease, plan delivery.PreparePlan, child delivery.ExportChildIntent) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if db == nil || lease.ParentTask != plan.TaskID() || f.stage < 1 || f.stage > 2 {
		return false, v2input.ErrInvalid
	}
	if _, _, err := plan.ExportRequest(lease.Run, child); err != nil {
		return false, err
	}
	draft, err := f.DraftTask()
	if err != nil {
		return false, err
	}
	workflow, err := v2input.WorkflowID(child.RunID)
	if err != nil {
		return false, err
	}
	id, err := v2input.TaskID(workflow, draft.Operation, 0)
	if err != nil || id != draft.ID {
		return false, v2input.ErrInvalid
	}
	task, err := canonical.Marshal(draft)
	if err != nil {
		return false, err
	}
	var ready bool
	err = db.QueryRow(ctx, `SELECT photo.v30_delivery_continuation_transport_ready($1,$2,$3,$4,$5,$6::jsonb,$7)`, lease.Expansion, lease.Token, child.SlotIndex, f.stage, f.predecessor, string(task), string(f.task.closure.ParamsJCS)).Scan(&ready)
	return ready, err
}
