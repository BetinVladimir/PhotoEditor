package workflow

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
)

type RenderBatchFinalizer struct {
	DB interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	}
}

func (f RenderBatchFinalizer) ProcessOne(ctx context.Context) (bool, error) {
	if f.DB == nil {
		return false, errors.New("render batch database required")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	var finalized, synchronized bool
	err := f.DB.QueryRow(ctx, `SELECT photo.finalize_v26_render_batch()`).Scan(&finalized)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return finalized, err
	}
	if err := ctx.Err(); err != nil {
		return finalized, err
	}
	// Run both bounded operations per pass so a steady terminal queue cannot
	// starve durable intermediate status updates for other batches.
	err = f.DB.QueryRow(ctx, `SELECT photo.sync_v26_render_batch_state()`).Scan(&synchronized)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return finalized || synchronized, err
}
