package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/v2input"
)

type MultiPreviewReceipt struct {
	Run          uuid.UUID `json:"run_id"`
	SourceResult uuid.UUID `json:"source_result_id"`
	State        string    `json:"state"`
	Preview1600  uuid.UUID `json:"preview1600_task_id"`
	Preview2560  uuid.UUID `json:"preview2560_task_id"`
}

func (s MultiPreviewIntentStore) ReadReceipt(ctx context.Context, plan MultiPreviewPlan) (MultiPreviewReceipt, bool, error) {
	var zero MultiPreviewReceipt
	if err := ctx.Err(); err != nil {
		return zero, false, err
	}
	if err := validateMultiPreviewPlan(plan); err != nil {
		return zero, false, err
	}
	value := MultiPreviewReceipt{Run: plan.run, SourceResult: plan.source.Binding.ResultID}
	found := true
	err := withMultiPreviewDocumentTx(ctx, s.DB, plan.source.Tenant, func(ctx context.Context, tx pgx.Tx) error {
		err := tx.QueryRow(ctx, "SELECT * FROM photo.v29_multi_preview_receipt($1,$2,$3,$4,$5,$6,$7,$8,$9)", plan.source.Tenant, plan.run, plan.source.Producer, plan.source.Binding.ResultID, plan.source.Binding.Result.SHA256, plan.srgb, plan.source.Binding.Operation, plan.source.Attempt, plan.source.Fence).Scan(&value.State, &value.Preview1600, &value.Preview2560)
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
	if err := validateMultiPreviewReceipt(plan, value); err != nil {
		return zero, false, err
	}
	return value, true, nil
}
func validateMultiPreviewReceipt(plan MultiPreviewPlan, value MultiPreviewReceipt) error {
	if value.Run != plan.run || value.SourceResult != plan.source.Binding.ResultID || value.Preview1600 != uuid.NewSHA1(plan.run, []byte("render.derivative:preview1600")) || value.Preview2560 != uuid.NewSHA1(plan.run, []byte("render.derivative:preview2560")) {
		return v2input.ErrInvalid
	}
	switch value.State {
	case "running", "completed", "completed_with_errors", "failed", "cancelled":
		return nil
	default:
		return v2input.ErrInvalid
	}
}
