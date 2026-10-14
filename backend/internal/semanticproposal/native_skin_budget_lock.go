package semanticproposal

import (
	"context"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Holds original authority, exact registered inputs and live run quota. This
// pre-seal gate does not activate a task; caller must roll back every failure.
func (s SQLTransferSourceReader) lockNativeSkinGraphBudgetInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, plan nativeSkinDraftPlan, color TransferColorProfile, built v2input.Built) error {
	if tx == nil {
		return v2input.ErrInvalid
	}
	if err := verifyNativeSkinPlanSeal(ctx, plan); err != nil {
		return err
	}
	registration, err := marshalNativeSkinRegistrationBinding(ctx, plan, built)
	if err != nil {
		return err
	}
	if _, err := s.beginNativeSkinDraftInTransaction(ctx, tx, who, plan, color); err != nil {
		return err
	}
	var state string
	if err := tx.QueryRow(ctx, "SELECT photo.semantic_skin_registration_binding_state($1,$2,$3,$4::jsonb)", plan.shell.Tenant, plan.shell.Run, "mask.face_skin", string(registration)).Scan(&state); err != nil {
		return err
	}
	if state != "replayed" {
		return v2input.ErrConflict
	}
	packet, err := canonical.Marshal(map[string]any{"operation": "mask.face_skin", "actor_id": who.UserID, "snapshot_id": plan.shell.Snapshot, "registration": map[string]any{"task_id": plan.shell.Tasks[0].ID}})
	if err != nil {
		return err
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_skin_graph_budget($1,$2,$3::jsonb)", plan.shell.Tenant, plan.shell.Run, string(packet)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
