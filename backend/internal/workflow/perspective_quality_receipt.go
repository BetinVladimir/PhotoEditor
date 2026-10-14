package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/v2input"
)

type PerspectiveQualityReceipt struct {
	Run          uuid.UUID `json:"run_id"`
	SourceResult uuid.UUID `json:"source_result_id"`
	State        string    `json:"state"`
	Quality      uuid.UUID `json:"quality_task_id"`
}

func (s PerspectiveQualityIntentStore) ReadReceipt(ctx context.Context, plan PerspectiveQualityPlan) (PerspectiveQualityReceipt, bool, error) {
	var zero PerspectiveQualityReceipt
	if err := ctx.Err(); err != nil {
		return zero, false, err
	}
	if err := validatePerspectiveQualityIntent(plan); err != nil {
		return zero, false, err
	}
	value := PerspectiveQualityReceipt{Run: plan.run, SourceResult: plan.source.ResultID}
	found := true
	err := withPerspectivePreviewDocumentTx(ctx, s.DB, plan.source.Tenant, func(ctx context.Context, tx pgx.Tx) error {
		err := tx.QueryRow(ctx, "SELECT * FROM photo.v29_perspective_quality_receipt($1,$2,$3,$4,$5)", plan.source.Tenant, plan.run, plan.source.Producer, plan.source.ResultID, plan.source.Result.SHA256).Scan(&value.State, &value.Quality)
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
	if err := validatePerspectiveQualityReceipt(plan, value); err != nil {
		return zero, false, err
	}
	return value, true, nil
}
func validatePerspectiveQualityReceipt(plan PerspectiveQualityPlan, value PerspectiveQualityReceipt) error {
	if value.Run != plan.run || value.SourceResult != plan.source.ResultID || value.Quality != uuid.NewSHA1(plan.run, []byte("quality.check:master")) {
		return v2input.ErrInvalid
	}
	switch value.State {
	case "running", "completed", "completed_with_errors", "failed", "cancelled":
		return nil
	default:
		return v2input.ErrInvalid
	}
}
