package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/v2input"
)

type PerspectivePreviewReceipt struct {
	Run          uuid.UUID `json:"run_id"`
	SourceResult uuid.UUID `json:"source_result_id"`
	State        string    `json:"state"`
	Preview1600  uuid.UUID `json:"preview1600_task_id"`
	Preview2560  uuid.UUID `json:"preview2560_task_id"`
}

func (s PerspectivePreviewIntentStore) ReadReceipt(ctx context.Context, plan PerspectivePreviewPlan) (PerspectivePreviewReceipt, bool, error) {
	var zero PerspectivePreviewReceipt
	if err := ctx.Err(); err != nil {
		return zero, false, err
	}
	if err := validatePerspectivePreviewIntent(plan); err != nil {
		return zero, false, err
	}
	value := PerspectivePreviewReceipt{Run: plan.run, SourceResult: plan.source.ResultID}
	found := true
	err := withPerspectivePreviewDocumentTx(ctx, s.DB, plan.source.Tenant, func(ctx context.Context, tx pgx.Tx) error {
		err := tx.QueryRow(ctx, "SELECT * FROM photo.v29_perspective_preview_receipt($1,$2,$3,$4,$5,$6)", plan.source.Tenant, plan.run, plan.source.Producer, plan.source.ResultID, plan.source.Result.SHA256, plan.srgb).Scan(&value.State, &value.Preview1600, &value.Preview2560)
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
	if err := validatePerspectivePreviewReceipt(plan, value); err != nil {
		return zero, false, err
	}
	return value, true, nil
}
func validatePerspectivePreviewReceipt(plan PerspectivePreviewPlan, value PerspectivePreviewReceipt) error {
	if value.Run != plan.run || value.SourceResult != plan.source.ResultID || value.Preview1600 != uuid.NewSHA1(plan.run, []byte("render.derivative:preview1600")) || value.Preview2560 != uuid.NewSHA1(plan.run, []byte("render.derivative:preview2560")) {
		return v2input.ErrInvalid
	}
	switch value.State {
	case "running", "completed", "completed_with_errors", "failed", "cancelled":
		return nil
	default:
		return v2input.ErrInvalid
	}
}
