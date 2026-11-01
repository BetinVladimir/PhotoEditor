package billing

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"time"
)

// PrepareOperations bounds the cold lock wait before graph/domain locks.
// The SQL result (cohort enabled) is intentionally not returned: billing_bind_operations,
// called inside the seal wrapper, re-runs billing_prepare_operations and returns NULL for
// disabled or already-covered runs. This call fixes lock order and fails early on authority.
func PrepareOperations(ctx context.Context, tx pgx.Tx, tenant, run uuid.UUID) error {
	cold, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	var enabled bool
	err := tx.QueryRow(cold, `SELECT photo.billing_prepare_operations($1,$2)`, tenant, run).Scan(&enabled)
	return checkColdLock(ctx, cold, err)
}
