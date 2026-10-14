package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Caller owns original parent lock and rollback. This completes canonical
// metadata only; downstream consumers must additionally verify physical bytes.
func (s SQLTransferSourceReader) finishNativeTargetDefectInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, original detectorDraftPlan, plan nativeTargetDefectDraftPlan, dependencies nativeTargetDefectDependencies, built v2input.Built, result uuid.UUID, outputs []v2input.Ref) error {
	if tx == nil || result == uuid.Nil || len(outputs) != 5 {
		return v2input.ErrInvalid
	}
	packet, err := s.marshalNativeTargetDefectAdmissionEvidence(ctx, who, original, plan, dependencies, built)
	if err != nil {
		return err
	}
	body, err := canonical.Marshal(outputs)
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
	if err := tx.QueryRow(ctx, "SELECT photo.finish_semantic_target_defect($1,$2,$3::jsonb,$4,$5::jsonb)", plan.shell.Tenant, plan.shell.Run, string(packet), result, string(body)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
