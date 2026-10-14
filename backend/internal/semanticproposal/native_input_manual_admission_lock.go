package semanticproposal

import (
	"context"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Composes the distinct complete manual packet in SQL. Caller rolls back every
// error. This lock does not activate the graph or expose a queued ticket.
func (s SQLTransferSourceReader) lockNativeInputManualAdmissionInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, plan nativeInputManualDraftPlan, built v2input.Built) error {
	packet, err := s.marshalNativeInputManualAdmissionEvidence(ctx, who, plan, built)
	if err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
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
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_native_manual_admission_evidence($1,$2,$3::jsonb)", plan.shell.Tenant, plan.shell.Run, string(packet)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
