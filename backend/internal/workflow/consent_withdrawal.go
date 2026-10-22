package workflow

import (
	"context"
	"photoeditor/backend/internal/taskrecovery"

	"github.com/jackc/pgx/v5"
)

type consentMaintenanceDB interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// Runs independently of optional inference/adoption installation. Only SQL
// current-proof witnesses request cancellation; adopter failures stay retryable.
func processConsentMaintenance(ctx context.Context, db consentMaintenanceDB, queries []string, limit int) (int, error) {
	return taskrecovery.ProcessConsentMaintenance(ctx, db, queries, limit)
}
