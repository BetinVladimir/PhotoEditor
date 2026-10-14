package v2optical

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"time"
)

// LockNegativeSource checks accepted original snapshot scope in the caller's
// scheduler transaction. Child array authority and graph publication remain
// separate requirements; this function performs no object IO or commit.
func LockNegativeSource(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant, project, snapshot uuid.UUID, paramsRaw, sourceRaw []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || tenant == uuid.Nil || project == uuid.Nil || snapshot == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return v2input.ErrInvalid
	}
	params, err := ParseNegativeParams(paramsRaw)
	if err != nil {
		return err
	}
	if _, err := VerifyNegativeWorkingImage(params, sourceRaw); err != nil {
		return err
	}
	base, err := json.Marshal(params.Base)
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
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return err
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.v28_local_portrait_source_scope($1,$2,$3,$4,$5,$6::jsonb)", tenant, project, snapshot, who.UserID, who.SessionID, string(base)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrExpired
	}
	return ctx.Err()
}
