package billing

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/domain"
	"time"
)

// PrepareOperations bounds the cold lock wait before graph/domain locks.
func PrepareOperations(ctx context.Context, tx pgx.Tx, tenant, run uuid.UUID) error {
	cold, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	var enabled bool
	err := tx.QueryRow(cold, `SELECT photo.billing_prepare_operations($1,$2)`, tenant, run).Scan(&enabled)
	if err != nil && ctx.Err() == nil && errors.Is(cold.Err(), context.DeadlineExceeded) {
		return domain.NewError("BILLING_UNAVAILABLE", 503)
	}
	return MapError(err)
}
