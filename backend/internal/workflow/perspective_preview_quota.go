package workflow

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"time"
)

const PerspectivePreviewReservedBytes int64 = 111149056

// Reserve requires an existing exact saved draft; it never creates one.
func (s PerspectivePreviewIntentStore) Reserve(ctx context.Context, plan PerspectivePreviewPlan) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := validatePerspectivePreviewIntent(plan); err != nil {
		return false, err
	}
	return s.previewQuota(ctx, plan.source.Tenant, plan.run, false)
}

// Release uses only the stored run kind and settled tasks/uploads/documents.
// It can clean up after source revocation without recreating source authority.
func (s PerspectivePreviewIntentStore) Release(ctx context.Context, tenant, run uuid.UUID) (bool, error) {
	return s.previewQuota(ctx, tenant, run, true)
}
func (s PerspectivePreviewIntentStore) previewQuota(ctx context.Context, tenant, run uuid.UUID, release bool) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if s.DB == nil || tenant == uuid.Nil || run == uuid.Nil {
		return false, v2input.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return false, err
	}
	if role != "photo_scheduler" {
		return false, v2input.ErrInvalid
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return false, err
	}
	query := "SELECT photo.reserve_v29_perspective_preview($1,$2)"
	if release {
		query = "SELECT photo.release_v29_perspective_preview_quota($1,$2)"
	}
	var accepted bool
	if err = tx.QueryRow(ctx, query, tenant, run).Scan(&accepted); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return accepted, nil
}
