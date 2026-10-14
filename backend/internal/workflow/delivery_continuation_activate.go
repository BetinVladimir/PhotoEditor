package workflow

import (
	"context"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// ActivateInTransaction requires already committed/registered frozen transport.
// SQL atomically seals the current runnable stage, closes its barrier, assigns
// encode ownership and acknowledges the verified predecessor. Caller rolls back
// every error. A durable activation receipt short-circuits dispatched retries.
func (f FrozenDeliveryContinuationTask) ActivateInTransaction(ctx context.Context, tx delivery.ProofTransaction, lease delivery.ExpansionLease, plan delivery.PreparePlan, child delivery.ExportChildIntent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || lease.ParentTask != plan.TaskID() {
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
	if err != nil || id != draft.ID || f.stage < 1 || f.stage > 2 {
		return v2input.ErrInvalid
	}
	task, err := canonical.Marshal(draft)
	if err != nil {
		return err
	}
	var activated bool
	err = tx.QueryRow(ctx, `SELECT photo.v30_delivery_continuation_activated($1,$2,$3,$4,$5,$6::jsonb,$7)`, lease.Expansion, lease.Token, child.SlotIndex, f.stage, f.predecessor, string(task), string(f.task.closure.ParamsJCS)).Scan(&activated)
	if err != nil || activated {
		return err
	}
	var accepted bool
	err = tx.QueryRow(ctx, `SELECT photo.activate_v30_delivery_continuation($1,$2,$3,$4,$5,$6::jsonb,$7)`, lease.Expansion, lease.Token, child.SlotIndex, f.stage, f.predecessor, string(task), string(f.task.closure.ParamsJCS)).Scan(&accepted)
	if err != nil {
		return err
	}
	if !accepted {
		return domain.NewError("DELIVERY_CONTINUATION_ACTIVATION_REFUSED", 409)
	}
	return nil
}

// ReopenContinuationInTransaction records the second derivative's physical
// verification and reopens its barrier before final encode materialization.
func (v VerifiedDeliveryStage) ReopenContinuationInTransaction(ctx context.Context, tx delivery.ProofTransaction, lease delivery.ExpansionLease, plan delivery.PreparePlan, child delivery.ExportChildIntent, bucket string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || lease.ParentTask != plan.TaskID() {
		return v2input.ErrInvalid
	}
	if _, _, err := plan.ExportRequest(lease.Run, child); err != nil {
		return err
	}
	workflow, err := v2input.WorkflowID(child.RunID)
	if err != nil {
		return err
	}
	task, err := v2input.TaskID(workflow, "derivative.sharpen", 0)
	if err != nil {
		return err
	}
	producer, err := canonical.Marshal(v.producer)
	if err != nil {
		return err
	}
	if _, err := parseDeliveryStageResult(producer, task, bucket); err != nil {
		return err
	}
	var accepted bool
	err = tx.QueryRow(ctx, `SELECT photo.reopen_v30_delivery_continuation($1,$2,$3,$4::jsonb,$5,$6)`, lease.Expansion, lease.Token, child.SlotIndex, string(producer), string(v.output.ImageJSON), string(v.output.ReportJSON)).Scan(&accepted)
	if err != nil {
		return err
	}
	if !accepted {
		return domain.NewError("DELIVERY_CONTINUATION_REOPEN_REFUSED", 409)
	}
	return nil
}
