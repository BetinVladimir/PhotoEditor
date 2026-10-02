package workflow

import (
	"context"
	"errors"
)

// SkipFailedInspection propagates only final verify failure/cancellation. Each
// function call commits one node; retry_wait is deliberately not terminal.
func (r IngestPlanRepository) SkipFailedInspection(ctx context.Context, limit int) (int, error) {
	if r.DB == nil || limit < 1 || limit > 100 {
		return 0, errors.New("invalid ingest failure batch")
	}
	count := 0
	for i := 0; i < limit; i++ {
		if err := ctx.Err(); err != nil {
			return count, err
		}
		var changed int
		if err := r.DB.QueryRow(ctx, "SELECT photo.skip_failed_ingest_inspection()").Scan(&changed); err != nil {
			return count, err
		}
		if changed != 0 && changed != 1 {
			return count, errors.New("invalid ingest failure transition count")
		}
		if changed == 0 {
			break
		}
		count++
	}
	return count, nil
}
