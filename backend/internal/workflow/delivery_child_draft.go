package workflow

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// CreateDraft commits only the initial blocked task/export shell. The caller
// owns the transaction and must roll it back on error. Transport publication
// belongs after commit, followed by fenced registration/sealing and cursor commit.
func (f FrozenDeliveryInitialTask) CreateDraft(ctx context.Context, tx delivery.ProofTransaction, lease delivery.ExpansionLease, plan delivery.PreparePlan, child delivery.ExportChildIntent, exportID uuid.UUID, reservedBytes int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	draft, err := f.DraftTask()
	if err != nil {
		return err
	}
	if tx == nil || lease.Expansion == uuid.Nil || lease.Token == uuid.Nil || lease.Tenant == uuid.Nil || lease.Package == uuid.Nil || lease.ParentTask != plan.TaskID() || child.SlotIndex > lease.Cursor || exportID == uuid.Nil || reservedBytes < 1 || reservedBytes > 9007199254740991 {
		return v2input.ErrInvalid
	}
	workflow, err := v2input.WorkflowID(lease.Run)
	if err != nil || workflow != lease.Workflow {
		return v2input.ErrInvalid
	}
	_, request, err := plan.ExportRequest(lease.Run, child)
	if err != nil {
		return err
	}
	childWorkflow, err := v2input.WorkflowID(child.RunID)
	if err != nil {
		return err
	}
	taskID, err := v2input.TaskID(childWorkflow, draft.Operation, 0)
	if err != nil || taskID != draft.ID {
		return v2input.ErrInvalid
	}
	config, _, err := f.childConfig(lease.Run, plan, child, exportID)
	if err != nil {
		return err
	}
	estimate, err := ParseResourceEstimate(draft.ExecutionProfile)
	if err != nil || reservedBytes < estimate.OutputBytes+int64(len(config)+len(f.closure.ParamsJCS)) {
		return v2input.ErrInvalid
	}
	task, err := canonical.Marshal(draft)
	if err != nil {
		return err
	}
	var accepted bool
	err = tx.QueryRow(ctx, `SELECT photo.create_v30_delivery_child_draft($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9)`,
		lease.Expansion, lease.Token, child.SlotIndex, string(request), exportID, string(config), string(task), string(f.closure.ParamsJCS), reservedBytes).Scan(&accepted)
	if err != nil {
		return err
	}
	if !accepted {
		return domain.NewError("DELIVERY_CHILD_ADMISSION_REFUSED", 409)
	}
	return nil
}
