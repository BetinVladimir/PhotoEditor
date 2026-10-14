package semanticproposal

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Commit the blocked owner before publishing objects. Complete registration and
// admission commit together after physical evidence rechecks. Failed publication
// leaves a deterministic blocked owner; sealed-owner replay uses saved recovery.
func (s SQLTransferSourceReader) startNativeInputManualInitialPreparation(ctx context.Context, scheduler *pgxpool.Pool, publisher v2input.Publisher, who auth.Identity, plan nativeInputManualInitialDraftPlan, color TransferColorProfile) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	if err := verifyNativeInputManualInitialDraftSeal(ctx, plan); err != nil {
		return zero, err
	}
	if scheduler == nil || publisher == nil {
		return zero, v2input.ErrInvalid
	}
	return coordinateNativeInputManualInitial(ctx,
		func() error {
			_, err := s.beginNativeInputManualDraft(ctx, scheduler, who, plan, color)
			return err
		},
		func() (v2input.PreparedSemanticTask, error) {
			prepared, _, err := s.publishNativeInputManualInitialTransport(ctx, who, plan, color, publisher)
			return prepared, err
		},
		func(built v2input.Built) (v2input.JobTicket, error) {
			return s.registerAndSealNativeInputManualInitial(ctx, scheduler, who, plan, color, built)
		})
}

// Both database callbacks return only after commit. No partial task or receipt
// escapes any failed phase, including an unknown final commit outcome.
func coordinateNativeInputManualInitial(ctx context.Context, begin func() error, publish func() (v2input.PreparedSemanticTask, error), admit func(v2input.Built) (v2input.JobTicket, error)) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if begin == nil || publish == nil || admit == nil {
		return zero, v2input.ErrInvalid
	}
	if err := begin(); err != nil {
		return zero, fmt.Errorf("commit initial manual draft: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	prepared, err := publish()
	if err != nil {
		return zero, fmt.Errorf("publish initial manual transport: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	ticket, err := admit(prepared.Graph.Built)
	if err != nil {
		return zero, fmt.Errorf("commit initial manual admission: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return ticket, nil
}
