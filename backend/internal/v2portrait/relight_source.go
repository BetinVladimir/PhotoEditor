package v2portrait

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// LockRelightSource retains accepted snapshot/source and actor authority in
// the caller transaction. Exact source bytes and producer checks are separate.
func LockRelightSource(ctx context.Context, tx pgx.Tx, who auth.Identity,
	tenant, project, snapshot uuid.UUID, base v2input.SessionVersionBinding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || tenant == uuid.Nil || project == uuid.Nil || snapshot == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil ||
		!validLocalBase(1, base, base.Source, base.Source, 0) {
		return v2input.ErrInvalid
	}
	body, err := canonical.Marshal(base)
	if err != nil {
		return err
	}
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	var ready bool
	if err = tx.QueryRow(ctx, "SELECT photo.lock_v28_relight_source($1,$2,$3,$4,$5,$6::jsonb)", tenant, project, snapshot, who.UserID, who.SessionID, string(body)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrExpired
	}
	return ctx.Err()
}
