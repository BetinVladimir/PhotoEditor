package semanticproposal

import (
	"context"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// Routing hint only, using the existing scoped saved-config reader. Existing
// parents must still pass full saved config/Params/snapshot and input recovery.
func (s SQLTransferSourceReader) hasNativeInputSavedParent(ctx context.Context, who auth.Identity, request Request) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if s.DB == nil || who.UserID != request.ActorID {
		return false, v2input.ErrInvalid
	}
	var exists bool
	err := tenant.WithTenant(ctx, s.DB, who, request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM photo.semantic_native_input_transfer_saved_config($1,$2,$3,$4,$5))", request.TenantID, s.ProjectID, who.UserID, who.SessionID, request.RunID).Scan(&exists)
	})
	if err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return exists, nil
}
