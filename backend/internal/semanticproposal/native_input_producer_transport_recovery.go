package semanticproposal

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

type nativeInputFaceProducerTransports struct {
	detector, landmarks, parser, skin v2input.Built
}

func verifyNativeInputFaceProducerPlans(ctx context.Context, who auth.Identity, plans nativeInputRestoredFaceProducers) error {
	if err := verifyDetectorPlanSeal(ctx, plans.detector); err != nil {
		return err
	}
	if err := verifyNativeFacePlanSeal(ctx, plans.landmarks); err != nil {
		return err
	}
	if err := verifyNativeFacePlanSeal(ctx, plans.parser); err != nil {
		return err
	}
	if err := verifyNativeSkinPlanSeal(ctx, plans.skin); err != nil {
		return err
	}
	d := plans.detector
	if who.UserID != d.request.ActorID || who.SessionID != d.prepared.source.session || who.UserID != d.prepared.source.actor || plans.landmarks.prepared.operation != "face.landmarks" || plans.parser.prepared.operation != "face.parse" || plans.landmarks.detector.seal != d.seal || plans.parser.detector.seal != d.seal || plans.skin.parser.seal != plans.parser.seal {
		return v2input.ErrConflict
	}
	for _, shell := range []v2input.DraftShell{d.shell, plans.landmarks.shell, plans.parser.shell, plans.skin.shell} {
		if shell.Tenant != d.request.TenantID || shell.Project != d.prepared.source.project || shell.Actor != who.UserID || len(shell.Tasks) != 1 {
			return v2input.ErrConflict
		}
	}
	return ctx.Err()
}

// One SQL-only transaction recovers the four original registered transports.
// No partial result, repair, publication, registration or activation is allowed.
func recoverNativeInputFaceProducerTransportsInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, plans nativeInputRestoredFaceProducers) (nativeInputFaceProducerTransports, error) {
	var zero nativeInputFaceProducerTransports
	if err := verifyNativeInputFaceProducerPlans(ctx, who, plans); err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	detector, err := recoverDetectorTransportInTransaction(ctx, tx, who, plans.detector)
	if err != nil {
		return zero, err
	}
	landmarks, err := recoverNativeFaceTransportInTransaction(ctx, tx, who, plans.landmarks)
	if err != nil {
		return zero, err
	}
	parser, err := recoverNativeFaceTransportInTransaction(ctx, tx, who, plans.parser)
	if err != nil {
		return zero, err
	}
	skin, err := recoverNativeSkinTransportInTransaction(ctx, tx, who, plans.skin)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return nativeInputFaceProducerTransports{detector: detector, landmarks: landmarks, parser: parser, skin: skin}, nil
}

func recoverNativeInputFaceProducerTransports(ctx context.Context, scheduler *pgxpool.Pool, who auth.Identity, plans nativeInputRestoredFaceProducers) (nativeInputFaceProducerTransports, error) {
	var zero, recovered nativeInputFaceProducerTransports
	if err := verifyNativeInputFaceProducerPlans(ctx, who, plans); err != nil {
		return zero, err
	}
	if scheduler == nil {
		return zero, v2input.ErrInvalid
	}
	err := database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
		value, err := recoverNativeInputFaceProducerTransportsInTransaction(ctx, tx, who, plans)
		if err != nil {
			return err
		}
		recovered = value
		return nil
	})
	// Commit failure (including unknown outcome) must not expose saved bindings.
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return recovered, nil
}
