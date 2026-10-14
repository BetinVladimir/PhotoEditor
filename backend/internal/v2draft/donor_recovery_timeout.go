package v2draft

import (
	"context"
	"github.com/jackc/pgx/v5"
	"time"
)

// Cleanup must survive request cancellation but must itself remain bounded.
func rollbackDonorRecovery(ctx context.Context, tx pgx.Tx) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(cleanup)
}
