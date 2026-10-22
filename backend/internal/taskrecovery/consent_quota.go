package taskrecovery

import "context"

// Each candidate commits independently through the existing scheduler pool.
// Ordinary cancellation and historical settlement have separate bounded turns.
func settleConsentQuotas(ctx context.Context, db consentMaintenanceDB, limit int) (int, error) {
	return ProcessConsentMaintenance(ctx, db, []string{
		"SELECT photo.release_next_glare_withdrawn_quota()",
		"SELECT photo.release_next_open_eyes_withdrawn_quota()",
		"SELECT photo.release_next_v23_cancelled_training_quota()",
	}, limit)
}
