package semanticproposal

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

// Persist only blocked drafts before publication. Failed publication/registration
// leaves recoverable deterministic slots; no SQL transaction spans object IO.
func (s SQLTransferSourceReader) persistDetectorPreparationInputs(ctx context.Context, scheduler *pgxpool.Pool, publisher v2input.Publisher, who auth.Identity, plan detectorDraftPlan, color TransferColorProfile) (v2input.PreparedSemanticTask, error) {
	return s.persistDetectorPreparationInputsMode(ctx, scheduler, publisher, who, plan, color, nil)
}

// Draft ownership and immutable manual restart selection commit together before
// any transport publication. Final admission repeats the exact same selection.
func (s SQLTransferSourceReader) persistInitialManualDetectorInputs(ctx context.Context, scheduler *pgxpool.Pool, publisher v2input.Publisher, who auth.Identity, plan detectorDraftPlan, color TransferColorProfile, pixels []v2input.Ref, diameter [2]float64) (v2input.PreparedSemanticTask, error) {
	if _, _, err := compileNativeManualSelection(ctx, who, plan, pixels, diameter); err != nil {
		return v2input.PreparedSemanticTask{}, err
	}
	selection := nativeInitialManualSelection{Pixels: append([]v2input.Ref(nil), pixels...), Diameter: diameter}
	return s.persistDetectorPreparationInputsMode(ctx, scheduler, publisher, who, plan, color, &selection)
}

func (s SQLTransferSourceReader) persistDetectorPreparationInputsMode(ctx context.Context, scheduler *pgxpool.Pool, publisher v2input.Publisher, who auth.Identity, plan detectorDraftPlan, color TransferColorProfile, manual *nativeInitialManualSelection) (v2input.PreparedSemanticTask, error) {
	if err := ctx.Err(); err != nil {
		return v2input.PreparedSemanticTask{}, err
	}
	if scheduler == nil || publisher == nil {
		return v2input.PreparedSemanticTask{}, v2input.ErrInvalid
	}
	return persistNativePreparationInputs(ctx,
		func(callback func(pgx.Tx) error) error { return database.InTransaction(ctx, scheduler, callback) },
		func(tx pgx.Tx) error {
			if _, err := s.beginDetectorDraftInTransaction(ctx, tx, who, plan, color); err != nil {
				return err
			}
			if manual != nil {
				return s.saveNativeManualSelectionInTransaction(ctx, tx, who, plan, manual.Pixels, manual.Diameter)
			}
			return nil
		},
		func() (v2input.PreparedSemanticTask, error) {
			task, _, err := publishDetectorDraftTransport(ctx, plan, publisher)
			return task, err
		},
		func(tx pgx.Tx, built v2input.Built) error {
			return s.registerDetectorInputsInTransaction(ctx, tx, who, plan, color, built)
		})
}
func (s SQLTransferSourceReader) persistPersonPreparationInputs(ctx context.Context, scheduler *pgxpool.Pool, publisher v2input.Publisher, who auth.Identity, plan personDraftPlan, color TransferColorProfile) (v2input.PreparedSemanticTask, error) {
	if err := ctx.Err(); err != nil {
		return v2input.PreparedSemanticTask{}, err
	}
	if scheduler == nil || publisher == nil {
		return v2input.PreparedSemanticTask{}, v2input.ErrInvalid
	}
	return persistNativePreparationInputs(ctx,
		func(callback func(pgx.Tx) error) error { return database.InTransaction(ctx, scheduler, callback) },
		func(tx pgx.Tx) error {
			_, err := s.beginPersonDraftInTransaction(ctx, tx, who, plan, color)
			return err
		},
		func() (v2input.PreparedSemanticTask, error) {
			task, _, err := publishPersonDraftTransport(ctx, plan, publisher)
			return task, err
		},
		func(tx pgx.Tx, built v2input.Built) error {
			return s.registerPersonInputsInTransaction(ctx, tx, who, plan, color, built)
		})
}

func persistNativePreparationInputs(ctx context.Context, transaction func(func(pgx.Tx) error) error, begin func(pgx.Tx) error, publish func() (v2input.PreparedSemanticTask, error), register func(pgx.Tx, v2input.Built) error) (v2input.PreparedSemanticTask, error) {
	var zero v2input.PreparedSemanticTask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if transaction == nil || begin == nil || publish == nil || register == nil {
		return zero, v2input.ErrInvalid
	}
	if err := transaction(begin); err != nil {
		return zero, fmt.Errorf("persist native preparation draft: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	prepared, err := publish()
	if err != nil {
		return zero, fmt.Errorf("publish native input closure: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if err := transaction(func(tx pgx.Tx) error { return register(tx, prepared.Graph.Built) }); err != nil {
		return zero, fmt.Errorf("register native input closure: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return prepared, nil
}

// Retry only SQL sealing. No usable ticket escapes a failed callback/commit.
func (s SQLTransferSourceReader) sealRegisteredDetectorPreparation(ctx context.Context, scheduler *pgxpool.Pool, who auth.Identity, plan detectorDraftPlan, built v2input.Built) (v2input.JobTicket, error) {
	if err := ctx.Err(); err != nil {
		return v2input.JobTicket{}, err
	}
	if scheduler == nil {
		return v2input.JobTicket{}, v2input.ErrInvalid
	}
	return nativeTicketWithRetry(ctx, scheduler.Begin, func(tx pgx.Tx) (v2input.JobTicket, error) {
		return s.sealDetectorPreparationInTransaction(ctx, tx, who, plan, built)
	})
}
func (s SQLTransferSourceReader) sealRegisteredPersonPreparation(ctx context.Context, scheduler *pgxpool.Pool, who auth.Identity, plan personDraftPlan, built v2input.Built) (v2input.JobTicket, error) {
	if err := ctx.Err(); err != nil {
		return v2input.JobTicket{}, err
	}
	if scheduler == nil {
		return v2input.JobTicket{}, v2input.ErrInvalid
	}
	return nativeTicketWithRetry(ctx, scheduler.Begin, func(tx pgx.Tx) (v2input.JobTicket, error) {
		return s.sealPersonPreparationInTransaction(ctx, tx, who, plan, built)
	})
}

// Resume only an already registered native preparation. Each retry rereads the
// saved transport and performs full sealing in the same transaction. Publication
// and draft creation are deliberately absent from this recovery boundary.
func (s SQLTransferSourceReader) resumeDetectorPreparation(ctx context.Context, scheduler *pgxpool.Pool, who auth.Identity, plan detectorDraftPlan) (v2input.JobTicket, error) {
	if err := ctx.Err(); err != nil {
		return v2input.JobTicket{}, err
	}
	if scheduler == nil {
		return v2input.JobTicket{}, v2input.ErrInvalid
	}
	if err := verifyDetectorPlanSeal(ctx, plan); err != nil {
		return v2input.JobTicket{}, err
	}
	return nativeTicketWithRetry(ctx, scheduler.Begin, func(tx pgx.Tx) (v2input.JobTicket, error) {
		return s.recoverAndSealDetectorInTransaction(ctx, tx, who, plan)
	})
}
func (s SQLTransferSourceReader) resumePersonPreparation(ctx context.Context, scheduler *pgxpool.Pool, who auth.Identity, plan personDraftPlan) (v2input.JobTicket, error) {
	if err := ctx.Err(); err != nil {
		return v2input.JobTicket{}, err
	}
	if scheduler == nil {
		return v2input.JobTicket{}, v2input.ErrInvalid
	}
	if err := verifyPersonPlanSeal(ctx, plan); err != nil {
		return v2input.JobTicket{}, err
	}
	return nativeTicketWithRetry(ctx, scheduler.Begin, func(tx pgx.Tx) (v2input.JobTicket, error) {
		return s.recoverAndSealPersonInTransaction(ctx, tx, who, plan)
	})
}

// Restore the registered original transport and atomically seal/enable in one
// transaction. Retries expose no ticket until commit is known to succeed.
func (s SQLTransferSourceReader) resumeInitialDetectorPreparation(ctx context.Context, scheduler *pgxpool.Pool, who auth.Identity, plan detectorDraftPlan) (v2input.JobTicket, error) {
	if err := ctx.Err(); err != nil {
		return v2input.JobTicket{}, err
	}
	if scheduler == nil {
		return v2input.JobTicket{}, v2input.ErrInvalid
	}
	if err := verifyDetectorPlanSeal(ctx, plan); err != nil {
		return v2input.JobTicket{}, err
	}
	return nativeTicketWithRetry(ctx, scheduler.Begin, func(tx pgx.Tx) (v2input.JobTicket, error) {
		built, err := recoverDetectorTransportInTransaction(ctx, tx, who, plan)
		if err != nil {
			return v2input.JobTicket{}, err
		}
		return s.sealAndEnableInitialDetectorInTransaction(ctx, tx, who, plan, built)
	})
}

// Explicit selection, detector admission and queue enable commit atomically.
func (s SQLTransferSourceReader) resumeInitialDetectorWithManualSelection(ctx context.Context, scheduler *pgxpool.Pool, who auth.Identity, plan detectorDraftPlan, pixels []v2input.Ref, diameter [2]float64) (v2input.JobTicket, error) {
	if err := ctx.Err(); err != nil {
		return v2input.JobTicket{}, err
	}
	if scheduler == nil {
		return v2input.JobTicket{}, v2input.ErrInvalid
	}
	if _, _, err := compileNativeManualSelection(ctx, who, plan, pixels, diameter); err != nil {
		return v2input.JobTicket{}, err
	}
	owned := append([]v2input.Ref(nil), pixels...)
	return nativeTicketWithRetry(ctx, scheduler.Begin, func(tx pgx.Tx) (v2input.JobTicket, error) {
		built, err := recoverDetectorTransportInTransaction(ctx, tx, who, plan)
		if err != nil {
			return v2input.JobTicket{}, err
		}
		if err := s.saveNativeManualSelectionInTransaction(ctx, tx, who, plan, owned, diameter); err != nil {
			return v2input.JobTicket{}, err
		}
		return s.sealAndEnableInitialDetectorInTransaction(ctx, tx, who, plan, built)
	})
}
