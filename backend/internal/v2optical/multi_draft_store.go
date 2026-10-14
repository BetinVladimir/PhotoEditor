package v2optical

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"time"
)

// SaveMultiDraft never creates runnable tasks or reserves output quota.
func SaveMultiDraft(ctx context.Context, db PerspectiveTransactions, who auth.Identity, tenant uuid.UUID, draft MultiDraft) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s, err := draft.Snapshot()
	if err != nil {
		return err
	}
	if db == nil || tenant == uuid.Nil || s.Tenant != tenant || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || s.Actor != who.UserID || s.Session != who.SessionID {
		return v2input.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return err
	}
	var saved bool
	if err = tx.QueryRow(ctx, "SELECT photo.save_v29_multi_draft($1,$2,$3,$4)", tenant, who.UserID, who.SessionID, draft.raw).Scan(&saved); err != nil {
		return err
	}
	if !saved {
		return v2input.ErrExpired
	}
	return tx.Commit(ctx)
}
