package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/v2input"
)

type MultiQualityReceipt struct {
	Run          uuid.UUID `json:"run_id"`
	SourceResult uuid.UUID `json:"source_result_id"`
	State        string    `json:"state"`
	Quality      uuid.UUID `json:"quality_task_id"`
}

func (s MultiQualityIntentStore) ReadReceipt(ctx context.Context, plan MultiQualityPlan) (MultiQualityReceipt, bool, error) {
	var zero MultiQualityReceipt
	if err := ctx.Err(); err != nil {
		return zero, false, err
	}
	if err := validateMultiQualityIntent(plan); err != nil {
		return zero, false, err
	}
	value := MultiQualityReceipt{Run: plan.run, SourceResult: plan.source.Binding.ResultID}
	found := true
	err := withMultiPreviewDocumentTx(ctx, s.DB, plan.source.Tenant, func(ctx context.Context, tx pgx.Tx) error {
		err := tx.QueryRow(ctx, "SELECT * FROM photo.v29_multi_quality_receipt($1,$2,$3,$4,$5,$6,$7,$8)", plan.source.Tenant, plan.run, plan.source.Producer, plan.source.Binding.ResultID, plan.source.Binding.Result.SHA256, plan.source.Binding.Operation, plan.source.Attempt, plan.source.Fence).Scan(&value.State, &value.Quality)
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
	if err := validateMultiQualityReceipt(plan, value); err != nil {
		return zero, false, err
	}
	return value, true, nil
}
func validateMultiQualityReceipt(plan MultiQualityPlan, value MultiQualityReceipt) error {
	if value.Run != plan.run || value.SourceResult != plan.source.Binding.ResultID || value.Quality != uuid.NewSHA1(plan.run, []byte("quality.check:master")) {
		return v2input.ErrInvalid
	}
	switch value.State {
	case "running", "completed", "completed_with_errors", "failed", "cancelled":
		return nil
	default:
		return v2input.ErrInvalid
	}
}
