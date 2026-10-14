package workflow

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/v2input"
	"time"
)

type MultiPreviewIntentStore struct{ DB *pgxpool.Pool }

// Save persists only an immutable draft intent. Its transaction is committed
// before any future Params object IO. No quota or queue activation occurs here.
func (s MultiPreviewIntentStore) Save(ctx context.Context, plan MultiPreviewPlan) (uuid.UUID, error) {
	if err := ctx.Err(); err != nil {
		return uuid.Nil, err
	}
	if err := validateMultiPreviewPlan(plan); err != nil {
		return uuid.Nil, err
	}
	if s.DB == nil {
		return uuid.Nil, v2input.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return uuid.Nil, err
	}
	if role != "photo_scheduler" {
		return uuid.Nil, v2input.ErrInvalid
	}
	pin := plan.source
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", pin.Tenant.String()); err != nil {
		return uuid.Nil, err
	}
	var result *uuid.UUID
	err = tx.QueryRow(ctx, "SELECT photo.save_v29_multi_preview_intent($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)", pin.Tenant, pin.Producer, pin.Binding.Operation, pin.Binding.ResultID, pin.Attempt, pin.Fence, pin.Binding.Result.ArtifactID, pin.Binding.Result.SHA256, plan.srgb, plan.run).Scan(&result)
	if err != nil {
		return uuid.Nil, err
	}
	if result == nil || *result != plan.run {
		return uuid.Nil, v2input.ErrExpired
	}
	if err = tx.Commit(ctx); err != nil {
		return uuid.Nil, err
	}
	return *result, nil
}
