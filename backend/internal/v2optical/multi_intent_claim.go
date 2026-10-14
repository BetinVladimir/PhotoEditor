package v2optical

import (
	"context"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2input"
)

// ClaimMultiIntent commits the canonical request binding before source/object IO.
// A claim is not task admission and carries no execution or response authority.
func ClaimMultiIntent(ctx context.Context, db PerspectiveTransactions, who auth.Identity, tenant, project uuid.UUID, operation, key string, intent MultiIntent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if db == nil || tenant == uuid.Nil || project == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || !idempotency.ValidKey(key) {
		return v2input.ErrInvalid
	}
	body, err := canonical.Marshal(intent)
	if err != nil {
		return err
	}
	if _, err := DecodeMultiIntent(operation, body); err != nil {
		return err
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
	if err := (multiParamsRepository{}).authorize(ctx, tx, artifactjson.Scope{Tenant: tenant}); err != nil {
		return err
	}
	var outcome string
	if err := tx.QueryRow(ctx, "SELECT photo.claim_v29_multi_intent($1,$2,$3,$4,$5,$6,$7)", tenant, project, who.UserID, who.SessionID, operation, key, string(body)).Scan(&outcome); err != nil {
		return err
	}
	switch outcome {
	case "claimed":
	case "conflict":
		return v2input.ErrConflict
	case "stale":
		return v2input.ErrExpired
	default:
		return v2input.ErrInvalid
	}
	return tx.Commit(ctx)
}
