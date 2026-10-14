package workflow

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/v2input"
)

func validateRestorationPreviewReceipt(plan RestorationPreviewPlan,
	value RestorationPreviewReceipt) error {
	if value.Run != plan.run || value.SourceResult != plan.source.ResultID ||
		value.Before1600 != restorationPreviewTaskID(plan.run, "before", "preview1600") ||
		value.Before2560 != restorationPreviewTaskID(plan.run, "before", "preview2560") ||
		value.After1600 != restorationPreviewTaskID(plan.run, "after", "preview1600") ||
		value.After2560 != restorationPreviewTaskID(plan.run, "after", "preview2560") {
		return v2input.ErrInvalid
	}
	switch value.State {
	case "running", "completed", "completed_with_errors", "failed", "cancelled":
		return nil
	default:
		return v2input.ErrInvalid
	}
}

func (s RestorationPreviewIntentStore) ReadReceipt(ctx context.Context,
	plan RestorationPreviewPlan) (RestorationPreviewReceipt, bool, error) {
	var zero RestorationPreviewReceipt
	if err := ctx.Err(); err != nil {
		return zero, false, err
	}
	if err := validateRestorationPreviewIntent(plan); err != nil {
		return zero, false, err
	}
	if s.DB == nil {
		return zero, false, v2input.ErrInvalid
	}
	value := RestorationPreviewReceipt{Run: plan.run, SourceResult: plan.source.ResultID}
	found := true
	err := withPerspectivePreviewDocumentTx(ctx, s.DB, plan.source.Tenant,
		func(ctx context.Context, tx pgx.Tx) error {
			err := tx.QueryRow(ctx,
				"SELECT * FROM photo.v28_restoration_preview_receipt($1,$2,$3,$4,$5,$6,$7)",
				plan.source.Tenant, plan.run, plan.source.Producer, plan.source.Review,
				plan.source.ResultID, plan.source.Result.SHA256, plan.srgb).
				Scan(&value.State, &value.Before1600, &value.Before2560,
					&value.After1600, &value.After2560)
			if errors.Is(err, pgx.ErrNoRows) {
				found = false
				return nil
			}
			return err
		})
	if err != nil {
		return zero, false, err
	}
	if !found {
		return zero, false, nil
	}
	if err := validateRestorationPreviewReceipt(plan, value); err != nil {
		return zero, false, err
	}
	return value, true, nil
}
