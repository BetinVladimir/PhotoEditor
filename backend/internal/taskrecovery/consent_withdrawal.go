package taskrecovery

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type consentMaintenanceDB interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// Runs independently of optional inference/adoption installation. Only SQL
// current-proof witnesses request cancellation; adopter failures stay retryable.
func ProcessConsentMaintenance(ctx context.Context, db consentMaintenanceDB, queries []string, limit int) (int, error) {
	count := 0
	for _, query := range queries {
		for i := 0; i < limit; i++ {
			if err := ctx.Err(); err != nil {
				return count, err
			}
			bounded, cancel := context.WithTimeout(ctx, 20*time.Second)
			var changed bool
			err := db.QueryRow(bounded, query).Scan(&changed)
			boundedErr := bounded.Err()
			cancel()
			if err != nil {
				return count, err
			}
			if boundedErr != nil {
				return count, boundedErr
			}
			if err := ctx.Err(); err != nil {
				return count, err
			}
			if !changed {
				break
			}
			count++
		}
	}
	return count, nil
}
