package workflow

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/v2input"
)

const PerspectiveQualityReservedBytes int64 = 2097152

func (s PerspectiveQualityIntentStore) Reserve(ctx context.Context, plan PerspectiveQualityPlan) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := validatePerspectiveQualityIntent(plan); err != nil {
		return false, err
	}
	return s.qualityQuota(ctx, plan.source.Tenant, plan.run, false)
}
func (s PerspectiveQualityIntentStore) Release(ctx context.Context, tenant, run uuid.UUID) (bool, error) {
	return s.qualityQuota(ctx, tenant, run, true)
}
func (s PerspectiveQualityIntentStore) qualityQuota(ctx context.Context, tenant, run uuid.UUID, release bool) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if tenant == uuid.Nil || run == uuid.Nil || s.DB == nil {
		return false, v2input.ErrInvalid
	}
	sql := "SELECT photo.reserve_v29_perspective_quality($1,$2)"
	if release {
		sql = "SELECT photo.release_v29_perspective_quality_quota($1,$2)"
	}
	var accepted bool
	err := withPerspectivePreviewDocumentTx(ctx, s.DB, tenant, func(ctx context.Context, tx pgx.Tx) error { return tx.QueryRow(ctx, sql, tenant, run).Scan(&accepted) })
	return accepted, err
}
