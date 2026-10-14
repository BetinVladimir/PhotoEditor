package workflow

import (
	"context"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// RecordInTransaction writes a verification receipt, not generic adoption.
// Caller owns commit/rollback. No object IO occurs inside this method.
func (v VerifiedDeliveryStage) RecordInTransaction(ctx context.Context, tx delivery.ProofTransaction, lease delivery.ExpansionLease, plan delivery.PreparePlan, child delivery.ExportChildIntent, operation, bucket string) error {
	return v.record(ctx, tx, lease, plan, child, operation, bucket, false)
}

// BeginContinuationInTransaction atomically records verification and opens the
// initial derivative continuation barrier. Caller must commit/rollback; no IO.
func (v VerifiedDeliveryStage) BeginContinuationInTransaction(ctx context.Context, tx delivery.ProofTransaction, lease delivery.ExpansionLease, plan delivery.PreparePlan, child delivery.ExportChildIntent, operation, bucket string) error {
	return v.record(ctx, tx, lease, plan, child, operation, bucket, true)
}
func (v VerifiedDeliveryStage) record(ctx context.Context, tx delivery.ProofTransaction, lease delivery.ExpansionLease, plan delivery.PreparePlan, child delivery.ExportChildIntent, operation, bucket string, begin bool) error {
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
	task, err := v2input.TaskID(workflow, operation, 0)
	if err != nil {
		return err
	}
	if operation != "derivative.resize" && operation != "derivative.sharpen" {
		return v2input.ErrInvalid
	}
	producer, err := canonical.Marshal(v.producer)
	if err != nil {
		return err
	}
	if _, err := parseDeliveryStageResult(producer, task, bucket); err != nil {
		return err
	}
	if len(v.output.ImageJSON) < 2 || len(v.output.ReportJSON) < 2 {
		return v2input.ErrInvalid
	}
	var accepted bool
	query := `SELECT photo.record_v30_delivery_stage_verification($1,$2,$3,$4,$5::jsonb,$6,$7)`
	if begin {
		query = `SELECT photo.begin_v30_delivery_continuation($1,$2,$3,$4,$5::jsonb,$6,$7)`
	}
	err = tx.QueryRow(ctx, query, lease.Expansion, lease.Token, child.SlotIndex, operation, string(producer), string(v.output.ImageJSON), string(v.output.ReportJSON)).Scan(&accepted)
	if err != nil {
		return err
	}
	if !accepted {
		return domain.NewError("DELIVERY_STAGE_VERIFICATION_REFUSED", 409)
	}
	return nil
}
