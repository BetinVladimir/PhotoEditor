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
	Restoration bool
}

func (f RenderBatchFinalizer) ProcessOne(ctx context.Context) (bool, error) {
	if f.DB == nil {
		return false, errors.New("render batch database required")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	var finalized, synchronized, selected bool
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
	if err != nil {
		return finalized || synchronized, err
	}
	if err := ctx.Err(); err != nil {
		return finalized || synchronized, err
	}
	err = f.DB.QueryRow(ctx, `SELECT photo.finalize_v26_render_selected_run()`).Scan(&selected)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	if err != nil {
		return finalized || synchronized || selected, err
	}
	if err := ctx.Err(); err != nil {
		return finalized || synchronized || selected, err
	}
	var openEyes bool
	err = f.DB.QueryRow(ctx, `SELECT photo.finalize_v28_open_eyes_selected_run()`).Scan(&openEyes)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	if err != nil {
		return finalized || synchronized || selected || openEyes, err
	}
	if err := ctx.Err(); err != nil {
		return finalized || synchronized || selected || openEyes, err
	}
	var openEyesMedia bool
	err = f.DB.QueryRow(ctx, `SELECT photo.enqueue_next_v28_open_eyes_before_media()`).Scan(&openEyesMedia)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	if err != nil || !f.Restoration {
		return finalized || synchronized || selected || openEyes || openEyesMedia, err
	}
	if err := ctx.Err(); err != nil {
		return finalized || synchronized || selected || openEyes || openEyesMedia, err
	}
	var restoration bool
	err = f.DB.QueryRow(ctx, `SELECT photo.finalize_v28_restoration_selected_run()`).Scan(&restoration)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return finalized || synchronized || selected || openEyes || openEyesMedia || restoration, err
}
