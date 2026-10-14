package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Caller holds original parent lock and owns rollback. Packet is produced by
// the physical result reader before the transaction; no object IO occurs here.
func (s SQLTransferSourceReader) finishNativeTargetSkinInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, plan nativeTargetSkinDraftPlan, packet json.RawMessage) error {
	if err := s.lockNativeCompletedTargetSkinInTransaction(ctx, tx, who, plan, packet); err != nil {
		return err
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.finish_semantic_target_skin($1,$2,$3::jsonb)", plan.shell.Tenant, plan.shell.Run, string(packet)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
