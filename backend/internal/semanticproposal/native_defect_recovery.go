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
func recoverNativeDefectTransportInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, plan nativeDefectDraftPlan) (v2input.Built, error) {
	var zero v2input.Built
	if err := verifyNativeDefectDraftSeal(ctx, plan); err != nil {
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
	if err := tx.QueryRow(ctx, "SELECT photo.semantic_native_defect_registered_transport($1,$2,$3,$4,$5)", plan.shell.Tenant, plan.shell.Run, task.ID, who.UserID, who.SessionID).Scan(&raw); err != nil {
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

// Recover exact registered defect transport and repeat complete dependency
// admission/sealing in the same retry transaction. Plan reconstruction is separate.
func (s SQLTransferSourceReader) resumeNativeDefectPreparation(ctx context.Context, scheduler *pgxpool.Pool, who auth.Identity, plan nativeDefectDraftPlan) (v2input.JobTicket, error) {
	if err := verifyNativeDefectDraftSeal(ctx, plan); err != nil {
		return v2input.JobTicket{}, err
	}
	if scheduler == nil {
		return v2input.JobTicket{}, v2input.ErrInvalid
	}
	return nativeTicketWithRetry(ctx, scheduler.Begin, func(tx pgx.Tx) (v2input.JobTicket, error) {
		built, err := recoverNativeDefectTransportInTransaction(ctx, tx, who, plan)
		if err != nil {
			return v2input.JobTicket{}, err
		}
		return s.sealNativeDefectPreparationInTransaction(ctx, tx, who, plan, built)
	})
}
