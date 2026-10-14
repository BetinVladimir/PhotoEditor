package semanticproposal

import (
	"context"
	"reflect"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

// Recovers only an existing registered original transport. No object IO or
// admission; the distinct manual draft is fully recompiled before any SQL.
func (s SQLTransferSourceReader) recoverNativeInputManualTransportInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, plan nativeInputManualDraftPlan) (v2input.Built, error) {
	var zero v2input.Built
	if err := verifyNativeInputManualDraftSeal(ctx, plan); err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	current, err := s.compileNativeInputManualDraft(ctx, who, plan.prepared.stage)
	if err != nil {
		return zero, err
	}
	if current.seal != plan.seal || !reflect.DeepEqual(current.shell, plan.shell) {
		return zero, v2input.ErrConflict
	}
	task, err := current.prepared.frozen.DraftTask()
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
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", current.shell.Tenant.String()); err != nil {
		return zero, err
	}
	var raw []byte
	if err := tx.QueryRow(ctx, "SELECT photo.semantic_native_defect_registered_transport($1,$2,$3,$4,$5)", current.shell.Tenant, current.shell.Run, task.ID, who.UserID, who.SessionID).Scan(&raw); err != nil {
		return zero, err
	}
	if len(raw) == 0 {
		return zero, v2input.ErrConflict
	}
	built, err := decodeNativeRecoveredTransport(raw, task.ID)
	if err != nil {
		return zero, err
	}
	if _, err := current.prepared.frozen.BindPublishedTransport(ctx, built); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return built, nil
}

func (s SQLTransferSourceReader) recoverNativeInputManualTransport(ctx context.Context, scheduler *pgxpool.Pool, who auth.Identity, plan nativeInputManualDraftPlan) (v2input.Built, error) {
	var zero, built v2input.Built
	if err := verifyNativeInputManualDraftSeal(ctx, plan); err != nil {
		return zero, err
	}
	if scheduler == nil {
		return zero, v2input.ErrInvalid
	}
	err := database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
		var err error
		built, err = s.recoverNativeInputManualTransportInTransaction(ctx, tx, who, plan)
		return err
	})
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return built, nil
}
