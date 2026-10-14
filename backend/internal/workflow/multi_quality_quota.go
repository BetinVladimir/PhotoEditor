package workflow

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/v2input"
)

const MultiQualityReservedBytes int64 = 2097152

func (s MultiQualityIntentStore) Reserve(ctx context.Context, plan MultiQualityPlan) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := validateMultiQualityIntent(plan); err != nil {
		return false, err
	}
	return s.qualityQuota(ctx, plan.source.Tenant, plan.run, false)
}
func (s MultiQualityIntentStore) Release(ctx context.Context, tenant, run uuid.UUID) (bool, error) {
	return s.qualityQuota(ctx, tenant, run, true)
}
func (s MultiQualityIntentStore) qualityQuota(ctx context.Context, tenant, run uuid.UUID, release bool) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if tenant == uuid.Nil || run == uuid.Nil || s.DB == nil {
		return false, v2input.ErrInvalid
	}
	sql := "SELECT photo.reserve_v29_multi_quality($1,$2)"
	if release {
		sql = "SELECT photo.release_v29_multi_quality_quota($1,$2)"
	}
	var accepted bool
	err := withMultiPreviewDocumentTx(ctx, s.DB, tenant, func(ctx context.Context, tx pgx.Tx) error { return tx.QueryRow(ctx, sql, tenant, run).Scan(&accepted) })
	return accepted, err
}
