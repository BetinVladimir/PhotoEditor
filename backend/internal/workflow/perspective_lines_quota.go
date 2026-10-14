package workflow

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Durable documents/closure plus one bounded result. This is tenant object
// storage reservation, not the worker's separate local scratch/RAM budget.
const PerspectiveLinesReservedBytes int64 = 4194304

func (s PerspectiveLinesIntentStore) Reserve(ctx context.Context, plan PerspectiveLinesPlan) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	params := review.ArtifactRef{ID: uuid.NewSHA1(plan.task, []byte("quota-params-validation")), SHA256: renderDerivativeBytesSHA(plan.params)}
	if _, err := PreparePerspectiveLinesTask(plan, params); err != nil {
		return false, err
	}
	return s.linesQuota(ctx, plan.scope.Tenant, plan.scope.Run, false)
}
func (s PerspectiveLinesIntentStore) Release(ctx context.Context, tenant, run uuid.UUID) (bool, error) {
	return s.linesQuota(ctx, tenant, run, true)
}
func (s PerspectiveLinesIntentStore) linesQuota(ctx context.Context, tenant, run uuid.UUID, release bool) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if tenant == uuid.Nil || run == uuid.Nil || s.DB == nil {
		return false, v2input.ErrInvalid
	}
	query := "SELECT photo.reserve_v29_perspective_line_detection($1,$2)"
	if release {
		query = "SELECT photo.release_v29_perspective_line_detection_quota($1,$2)"
	}
	var accepted bool
	err := withPerspectivePreviewDocumentTx(ctx, s.DB, tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, query, tenant, run).Scan(&accepted)
	})
	return accepted, err
}
