package semanticproposal

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

// Commit blocked metadata before object publication. Registration repeats the
// original authority in a new transaction; no task is activated by this helper.
func (s SQLTransferSourceReader) persistNativeSkinPreparationInputs(ctx context.Context, scheduler *pgxpool.Pool, publisher v2input.Publisher, who auth.Identity, plan nativeSkinDraftPlan, color TransferColorProfile) (v2input.PreparedSemanticTask, error) {
	if err := verifyNativeSkinPlanSeal(ctx, plan); err != nil {
		return v2input.PreparedSemanticTask{}, err
	}
	if scheduler == nil || publisher == nil {
		return v2input.PreparedSemanticTask{}, v2input.ErrInvalid
	}
	return persistNativePreparationInputs(ctx,
		func(callback func(pgx.Tx) error) error { return database.InTransaction(ctx, scheduler, callback) },
		func(tx pgx.Tx) error {
			_, err := s.beginNativeSkinDraftInTransaction(ctx, tx, who, plan, color)
			return err
		},
		func() (v2input.PreparedSemanticTask, error) {
			task, _, err := publishNativeSkinDraftTransport(ctx, plan, publisher)
			return task, err
		},
		func(tx pgx.Tx, built v2input.Built) error {
			return s.registerNativeSkinInputsInTransaction(ctx, tx, who, plan, color, built)
		})
}
