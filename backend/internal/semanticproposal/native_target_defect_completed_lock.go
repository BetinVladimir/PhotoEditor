package semanticproposal

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Packet must be byte-verified before opening the transaction. This adapter
// performs SQL only, locks original authority and canonical completed outputs,
// and never grants admission by itself. Caller rolls back every error.
func (s SQLTransferSourceReader) lockNativeCompletedTargetDefectInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, plan nativeTargetDefectDraftPlan, packet json.RawMessage) error {
	if err := verifyNativeTargetDefectPlanSeal(ctx, plan); err != nil {
		return err
	}
	if tx == nil || len(packet) < 2 || len(packet) > 32<<20 {
		return v2input.ErrInvalid
	}
	owned, err := s.compileNativeTargetDefectDraftPlan(ctx, who, plan.prepared, plan.shell.Bytes)
	if err != nil {
		return err
	}
	if owned.seal != plan.seal {
		return v2input.ErrConflict
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
	r := plan.anchor.request
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_target_defect_result($1,$2,$3,$4,$5,$6,$7,$8::jsonb)", r.TenantID, s.ProjectID, who.UserID, who.SessionID, r.RunID, r.RequestSHA, plan.shell.Tasks[0].ID, string(packet)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
