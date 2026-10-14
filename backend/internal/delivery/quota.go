package delivery

import (
	"context"
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
	var released bool
	if err := s.DB.QueryRow(ctx, `SELECT photo.cleanup_next_v30_delivery()`).Scan(&released); err != nil {
		return false, err
	}
	return released, nil
}
