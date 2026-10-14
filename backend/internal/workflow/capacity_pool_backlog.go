package workflow

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

type CapacityBacklogQueries interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// ReadCapacityPoolPendingBacklog includes ready, queued, running and retrying v2
// work even when no qualified node can currently run it. It is an idle veto, not scale-up
// eligibility evidence.
func ReadCapacityPoolPendingBacklog(ctx context.Context, db CapacityBacklogQueries, pool string) (bool, error) {
	if db == nil || (pool != "cpu" && pool != "gpu" && pool != "signer") {
		return false, errors.New("invalid capacity backlog source")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	var count int64
	if err := db.QueryRow(ctx, "SELECT photo.capacity_pool_pending_backlog($1)", pool).Scan(&count); err != nil {
		return false, err
	}
	if count < 0 {
		return false, errors.New("negative capacity backlog")
	}
	return count > 0, nil
}
