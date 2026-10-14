package semanticproposal

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Recover only existing registered transport. Missing/partial state is conflict,
// never permission to publish, recreate a draft or repair its saved inputs.
func recoverNativeFaceTransportInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, plan nativeFaceDraftPlan) (v2input.Built, error) {
	var zero v2input.Built
	if err := verifyNativeFacePlanSeal(ctx, plan); err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	task, err := plan.prepared.frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return zero, err
	}
	if role != "photo_scheduler" {
		return zero, v2input.ErrInvalid
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", plan.shell.Tenant.String()); err != nil {
		return zero, err
	}
	var raw []byte
	if err := tx.QueryRow(ctx, "SELECT photo.semantic_face_registered_transport($1,$2,$3,$4,$5)", plan.shell.Tenant, plan.shell.Run, task.ID, who.UserID, who.SessionID).Scan(&raw); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if len(raw) == 0 {
		return zero, v2input.ErrConflict
	}
	built, err := decodeNativeRecoveredTransport(raw, task.ID)
	if err != nil {
		return zero, err
	}
	if _, err := plan.prepared.frozen.BindPublishedTransport(ctx, built); err != nil {
		return zero, err
	}
	return built, nil
}

// Each retry rereads both saved transports and seals under the same transaction.
// No usable ticket escapes a failed callback, commit or unknown commit outcome.
func (s SQLTransferSourceReader) resumeNativeFacePreparation(ctx context.Context, scheduler *pgxpool.Pool, who auth.Identity, plan nativeFaceDraftPlan, completed completedNativeDetector) (v2input.JobTicket, error) {
	if err := verifyNativeFacePlanSeal(ctx, plan); err != nil {
		return v2input.JobTicket{}, err
	}
	if scheduler == nil {
		return v2input.JobTicket{}, v2input.ErrInvalid
	}
	return nativeTicketWithRetry(ctx, scheduler.Begin, func(tx pgx.Tx) (v2input.JobTicket, error) {
		// Retain original dependency locks before the downstream graph, consistent
		// with composed admission and independent detector-result writers.
		parent, err := recoverDetectorTransportInTransaction(ctx, tx, who, plan.detector)
		if err != nil {
			return v2input.JobTicket{}, err
		}
		built, err := recoverNativeFaceTransportInTransaction(ctx, tx, who, plan)
		if err != nil {
			return v2input.JobTicket{}, err
		}
		return s.sealNativeFacePreparationInTransaction(ctx, tx, who, plan, completed, built, parent)
	})
}
