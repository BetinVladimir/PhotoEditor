package delivery

import (
	"context"
 "github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// ReleaseNextQuota synchronizes one cancelled package and settles one eligible
// terminal reservation. True means either transition committed; SQL rechecks
// task/upload/document cleanup before releasing bytes.
func (s ExpansionLeaseStore) ReleaseNextQuota(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if s.DB == nil {
		return false, v2input.ErrInvalid
	}
	changed := false
	if s.Instance != (uuid.UUID{}) && len(s.Bucket) >= 3 && len(s.Bucket) <= 63 {
		if err := s.DB.QueryRow(ctx, `SELECT photo.fail_next_v30_delivery_missing_source($1,$2)`, s.Instance, s.Bucket).Scan(&changed); err != nil {
			return false, err
		}
		if err := ctx.Err(); err != nil { return changed, err }
	}
	var released bool
	if err := s.DB.QueryRow(ctx, `SELECT photo.cleanup_next_v30_delivery()`).Scan(&released); err != nil {
		return changed, err
	}
	return changed || released, ctx.Err()
}
