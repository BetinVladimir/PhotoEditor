package workflow

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
)

type RenderDerivativeCleanup struct {
	DB interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	}
}

func (p RenderDerivativeCleanup) ProcessOne(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if p.DB == nil {
		return false, errors.New("derivative cleanup database required")
	}
	var released bool
	var finalized bool
	var completed bool
	var cancelled bool
	if err := p.DB.QueryRow(ctx, "SELECT photo.cancel_stale_v26_render_derivative()").Scan(&cancelled); err != nil {
		return false, err
	}
	if err := p.DB.QueryRow(ctx, "SELECT photo.finalize_successful_v26_render_derivative()").Scan(&completed); err != nil {
		return false, err
	}
	if err := p.DB.QueryRow(ctx, "SELECT photo.finalize_failed_v26_render_derivative()").Scan(&finalized); err != nil {
		return false, err
	}
	err := p.DB.QueryRow(ctx, "SELECT photo.release_next_v26_render_derivative_quota()").Scan(&released)
	return cancelled || completed || finalized || released, err
}
