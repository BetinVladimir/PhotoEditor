package workflow

import (
	"context"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// Twice the two-task V29 preview budget: two sides, each with both variants.
const RestorationPreviewReservedBytes int64 = 222298112

// Reserve requires a saved, current four-side draft and does not create one.
func (s RestorationPreviewIntentStore) Reserve(ctx context.Context,
	plan RestorationPreviewPlan) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := validateRestorationPreviewIntent(plan); err != nil {
		return false, err
	}
	return s.previewQuota(ctx, plan.source.Tenant, plan.run, false)
}

// Release acts on settled preview-owned resources and does not recreate source
// authority after the human review is decided or the original run is revoked.
func (s RestorationPreviewIntentStore) Release(ctx context.Context,
	tenant, run uuid.UUID) (bool, error) {
	return s.previewQuota(ctx, tenant, run, true)
}

func (s RestorationPreviewIntentStore) previewQuota(ctx context.Context,
	tenant, run uuid.UUID, release bool) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if s.DB == nil || tenant == uuid.Nil || run == uuid.Nil {
		return false, v2input.ErrInvalid
	}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.DB.Begin(bounded)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(context.WithoutCancel(bounded))
	var role string
	if err = tx.QueryRow(bounded, "SELECT current_user").Scan(&role); err != nil {
		return false, err
	}
	if role != "photo_scheduler" {
		return false, v2input.ErrInvalid
	}
	if _, err = tx.Exec(bounded, "SELECT set_config('app.tenant_id',$1,true)",
		tenant.String()); err != nil {
		return false, err
	}
	query := "SELECT photo.reserve_v28_restoration_preview($1,$2)"
	if release {
		query = "SELECT photo.release_v28_restoration_preview_quota($1,$2)"
	}
	var accepted bool
	if err = tx.QueryRow(bounded, query, tenant, run).Scan(&accepted); err != nil {
		return false, err
	}
	if err = tx.Commit(bounded); err != nil {
		return false, err
	}
	return accepted, nil
}
