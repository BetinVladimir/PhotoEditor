package billing

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"time"
)

// Render staging already needs an exclusive tenant lock; acquire it before the
// resource mutex to avoid upgrades while concurrent admissions hold SHARE locks.
func PrepareRender(ctx context.Context, tx pgx.Tx, tenant, project uuid.UUID, who auth.Identity) (bool, error) {
	cold, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	var enabled bool
	err := tx.QueryRow(cold, `SELECT photo.billing_prepare_render($1,$2,$3,$4)`, tenant, who.UserID, who.SessionID, project).Scan(&enabled)
	return enabled, checkColdLock(ctx, cold, err)
}
func BindRender(ctx context.Context, tx pgx.Tx, tenant, root uuid.UUID, who auth.Identity) error {
	var request *uuid.UUID
	err := tx.QueryRow(ctx, `SELECT photo.billing_bind_render($1,$2,$3,$4,$5)`, tenant, root, who.UserID, who.SessionID, instanceID).Scan(&request)
	return MapError(err)
}
