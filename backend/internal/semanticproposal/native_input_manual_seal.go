package semanticproposal

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Caller must commit before exposing this ticket and roll back every error.
func (s SQLTransferSourceReader) sealNativeInputManualPreparationInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, plan nativeInputManualDraftPlan, built v2input.Built) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	packet, err := s.marshalNativeInputManualAdmissionEvidence(ctx, who, plan, built)
	if err != nil {
		return zero, err
	}
	return sealNativeInputManualPacketInTransaction(ctx, tx, plan.shell, packet)
}

// The packet must come from an independently validated initial or restored plan.
func sealNativeInputManualPacketInTransaction(ctx context.Context, tx pgx.Tx, shell v2input.DraftShell, packet json.RawMessage) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return zero, err
	}
	if role != "photo_scheduler" {
		return zero, v2input.ErrInvalid
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", shell.Tenant.String()); err != nil {
		return zero, err
	}
	var raw []byte
	if err := tx.QueryRow(ctx, "SELECT photo.seal_semantic_native_manual_preparation($1,$2,$3::jsonb)", shell.Tenant, shell.Run, string(packet)).Scan(&raw); err != nil {
		return zero, err
	}
	if len(raw) == 0 {
		return zero, v2input.ErrConflict
	}
	if len(raw) > 4096 {
		return zero, v2input.ErrInvalid
	}
	var ticket v2input.JobTicket
	if json.Unmarshal(raw, &ticket) != nil || ticket.RunID != shell.Run || ticket.WorkflowID != shell.WorkflowID || ticket.State != "queued" {
		return zero, v2input.ErrInvalid
	}
	var enabled bool
	if err := tx.QueryRow(ctx, "SELECT photo.enable_v27_native_manual_parent($1,$2,$3::jsonb)", shell.Tenant, shell.Run, string(packet)).Scan(&enabled); err != nil {
		return zero, err
	}
	if !enabled {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return ticket, nil
}

// Physical qualification/manual/native result rechecks precede SQL-only retry
// transactions. Every retry recovers original transport and repeats complete
// admission even for a stored receipt. No missing state is recreated.
func (s SQLTransferSourceReader) resumeNativeInputManualPreparation(ctx context.Context, scheduler *pgxpool.Pool, who auth.Identity, plan nativeInputManualDraftPlan, color TransferColorProfile) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	if err := verifyNativeInputManualDraftSeal(ctx, plan); err != nil {
		return zero, err
	}
	if scheduler == nil {
		return zero, v2input.ErrInvalid
	}
	stage := plan.prepared.stage
	current, err := s.compileNativeInputManualDraft(ctx, who, stage)
	if err != nil {
		return zero, err
	}
	if current.seal != plan.seal || !reflect.DeepEqual(current.shell, plan.shell) {
		return zero, v2input.ErrConflict
	}
	qualified, err := s.prepareNativeInputManualStageQualification(ctx, who, stage.dependencies, color, *stage.saved.recovery.Report, stage.saved.recovery.Costs)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(qualified, stage.qualification) {
		return zero, v2input.ErrConflict
	}
	return nativeTicketWithRetry(ctx, scheduler.Begin, func(tx pgx.Tx) (v2input.JobTicket, error) {
		built, err := s.recoverNativeInputManualTransportInTransaction(ctx, tx, who, plan)
		if err != nil {
			return zero, err
		}
		return s.sealNativeInputManualPreparationInTransaction(ctx, tx, who, plan, built)
	})
}
