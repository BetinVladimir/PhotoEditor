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
	OperationBilling bool
	Billing          bool
	Restoration      bool
	LocalPortrait    bool
	Relight          bool
}

func (f RenderBatchFinalizer) ProcessOne(ctx context.Context) (bool, error) {
	if f.DB == nil {
		return false, errors.New("render batch database required")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	var operationChanged bool
	if f.OperationBilling {
		if err := f.DB.QueryRow(ctx, `SELECT photo.billing_settle_next_operations()`).Scan(&operationChanged); err != nil {
			return false, err
		}
	}
	billingChanged := operationChanged
	if f.Billing {
		var renderChanged bool
		err := f.DB.QueryRow(ctx, `SELECT photo.billing_settle_next_render()`).Scan(&renderChanged)
		billingChanged = billingChanged || renderChanged
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return billingChanged, err
		}
	}
	var finalized, synchronized, selected bool
	finalized = billingChanged
	err := f.DB.QueryRow(ctx, `SELECT photo.finalize_v26_render_batch()`).Scan(&finalized)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return billingChanged || finalized, err
	}
	if err := ctx.Err(); err != nil {
		return billingChanged || finalized, err
	}
	// Run both bounded operations per pass so a steady terminal queue cannot
	// starve durable intermediate status updates for other batches.
	err = f.DB.QueryRow(ctx, `SELECT photo.sync_v26_render_batch_state()`).Scan(&synchronized)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	if err != nil {
		return billingChanged || finalized || synchronized, err
	}
	if err := ctx.Err(); err != nil {
		return billingChanged || finalized || synchronized, err
	}
	err = f.DB.QueryRow(ctx, `SELECT photo.finalize_v26_render_selected_run()`).Scan(&selected)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	if err != nil {
		return billingChanged || finalized || synchronized || selected, err
	}
	if err := ctx.Err(); err != nil {
		return billingChanged || finalized || synchronized || selected, err
	}
	var openEyes bool
	err = f.DB.QueryRow(ctx, `SELECT photo.finalize_v28_open_eyes_selected_run()`).Scan(&openEyes)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	if err != nil {
		return billingChanged || finalized || synchronized || selected || openEyes, err
	}
	if err := ctx.Err(); err != nil {
		return billingChanged || finalized || synchronized || selected || openEyes, err
	}
	var openEyesMedia bool
	err = f.DB.QueryRow(ctx, `SELECT photo.enqueue_next_v28_open_eyes_before_media()`).Scan(&openEyesMedia)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	progress := operationChanged || billingChanged || finalized || synchronized || selected || openEyes || openEyesMedia
	if err != nil {
		return progress, err
	}
	if f.LocalPortrait {
		if err := ctx.Err(); err != nil {
			return progress, err
		}
		var changed bool
		err = f.DB.QueryRow(ctx, "SELECT photo.enqueue_next_v28_local_portrait_before_media()").Scan(&changed)
		progress = progress || changed
		if errors.Is(err, pgx.ErrNoRows) {
			err = nil
		}
		if err != nil {
			return progress, err
		}
	}
	if f.Relight {
		for _, statement := range []string{
			"SELECT photo.enqueue_next_v28_relight_before_media()",
			"SELECT photo.finalize_v28_relight_selected_run()",
		} {
			if err := ctx.Err(); err != nil {
				return progress, err
			}
			var changed bool
			err = f.DB.QueryRow(ctx, statement).Scan(&changed)
			progress = progress || changed
			if errors.Is(err, pgx.ErrNoRows) {
				err = nil
			}
			if err != nil {
				return progress, err
			}
		}
	}
	if !f.Restoration {
		return progress, nil
	}
	if err := ctx.Err(); err != nil {
		return progress, err
	}
	var restoration bool
	err = f.DB.QueryRow(ctx, `SELECT photo.finalize_v28_restoration_selected_run()`).Scan(&restoration)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return progress || restoration, err
}
