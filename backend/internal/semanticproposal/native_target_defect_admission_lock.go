package semanticproposal

import (
	"context"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Holds the complete downstream and original detector evidence until caller
// commit. No object IO or graph activation occurs in this admission barrier.
func (s SQLTransferSourceReader) lockNativeTargetDefectAdmissionInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, original detectorDraftPlan, plan nativeTargetDefectDraftPlan, dependencies nativeTargetDefectDependencies, built v2input.Built) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	packet, err := s.marshalNativeTargetDefectAdmissionEvidence(ctx, who, original, plan, dependencies, built)
	if err != nil {
		return err
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", plan.shell.Tenant.String()); err != nil {
		return err
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_target_defect_admission_evidence($1,$2,$3::jsonb)", plan.shell.Tenant, plan.shell.Run, string(packet)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
