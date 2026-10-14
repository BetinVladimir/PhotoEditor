package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// The caller commits before exposing a ticket and rolls back every error.
// Current evidence is repeated by SQL for both first seal and receipt replay.
func (s SQLTransferSourceReader) sealNativeDefectPreparationInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, plan nativeDefectDraftPlan, built v2input.Built) (v2input.JobTicket, error) {
	packet, err := s.marshalNativeDefectAdmissionEvidence(ctx, who, plan, built)
	if err != nil {
		return v2input.JobTicket{}, err
	}
	return sealNativeDefectPacketInTransaction(ctx, tx, plan.shell.Tenant, plan.shell.Run, plan.shell.WorkflowID, packet)
}

func (s SQLTransferSourceReader) sealRegisteredNativeDefectPreparation(ctx context.Context, scheduler *pgxpool.Pool, who auth.Identity, plan nativeDefectDraftPlan, built v2input.Built) (v2input.JobTicket, error) {
	if err := ctx.Err(); err != nil {
		return v2input.JobTicket{}, err
	}
	if scheduler == nil {
		return v2input.JobTicket{}, v2input.ErrInvalid
	}
	return nativeTicketWithRetry(ctx, scheduler.Begin, func(tx pgx.Tx) (v2input.JobTicket, error) {
		return s.sealNativeDefectPreparationInTransaction(ctx, tx, who, plan, built)
	})
}

func sealNativeDefectPacketInTransaction(ctx context.Context, tx pgx.Tx, tenant, run, workflow uuid.UUID, evidence json.RawMessage) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if tx == nil || tenant == uuid.Nil || run == uuid.Nil || workflow == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return zero, err
	}
	if role != "photo_scheduler" {
		return zero, v2input.ErrInvalid
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return zero, err
	}
	var raw []byte
	if err := tx.QueryRow(ctx, "SELECT photo.seal_semantic_native_defect_preparation($1,$2,$3::jsonb)", tenant, run, string(evidence)).Scan(&raw); err != nil {
		return zero, err
	}
	if len(raw) == 0 {
		return zero, v2input.ErrConflict
	}
	if len(raw) > 4096 {
		return zero, v2input.ErrInvalid
	}
	var ticket v2input.JobTicket
	if json.Unmarshal(raw, &ticket) != nil || ticket.RunID != run || ticket.WorkflowID != workflow || ticket.State != "queued" {
		return zero, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return ticket, nil
}
