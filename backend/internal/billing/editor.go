package billing

import (
	"context"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"time"
)

// Editor staging already needs an exclusive tenant lock; acquire it before the
// resource mutex to avoid upgrades while concurrent admissions hold SHARE locks.
func PrepareEditor(ctx context.Context, tx pgx.Tx, tenant, project uuid.UUID, who auth.Identity) (bool, error) {
	cold, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	var enabled bool
	err := tx.QueryRow(cold, `SELECT photo.billing_prepare_editor($1,$2,$3,$4)`, tenant, who.UserID, who.SessionID, project).Scan(&enabled)
	if err != nil && ctx.Err() == nil && errors.Is(cold.Err(), context.DeadlineExceeded) {
		return false, domain.NewError("BILLING_UNAVAILABLE", 503)
	}
	return enabled, MapError(err)
}
func BindEditor(ctx context.Context, tx pgx.Tx, tenant, root uuid.UUID, who auth.Identity, key, route string, request any) error {
	hash, err := canonical.Hash(request)
	if err != nil {
		return err
	}
	digest, err := hex.DecodeString(hash)
	if err != nil {
		return err
	}
	var requestID *uuid.UUID
	err = tx.QueryRow(ctx, `SELECT photo.billing_bind_editor($1,$2,$3,$4,$5,$6,$7,$8)`, tenant, root, who.UserID, who.SessionID, key, route, digest, instanceID).Scan(&requestID)
	return MapError(err)
}
