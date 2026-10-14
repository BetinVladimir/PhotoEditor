package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Publication must finish outside this transaction. Caller commits only after
// success and rolls back every failure, including malformed SQL receipts.
func (s SQLTransferSourceReader) sealDetectorPreparationInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, plan detectorDraftPlan, built v2input.Built) (v2input.JobTicket, error) {
	evidence, err := s.marshalDetectorAdmissionEvidence(ctx, who, plan, built)
	if err != nil {
		return v2input.JobTicket{}, err
	}
	return sealNativePreparationInTransaction(ctx, tx, plan.request.TenantID, plan.shell.Run, plan.shell.WorkflowID, evidence)
}
func (s SQLTransferSourceReader) sealPersonPreparationInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, plan personDraftPlan, built v2input.Built) (v2input.JobTicket, error) {
	evidence, err := s.marshalPersonAdmissionEvidence(ctx, who, plan, built)
	if err != nil {
		return v2input.JobTicket{}, err
	}
	return sealNativePreparationInTransaction(ctx, tx, plan.request.TenantID, plan.shell.Run, plan.shell.WorkflowID, evidence)
}

// Dedicated SQL sealing owns complete current evidence locks and sealed replay.
// Never short-circuit that validation using an independently read old ticket.
func sealNativePreparationInTransaction(ctx context.Context, tx pgx.Tx, tenant, run, workflow uuid.UUID, evidence json.RawMessage) (v2input.JobTicket, error) {
	return sealNativePreparationQueryInTransaction(ctx, tx, tenant, run, workflow, evidence, "SELECT photo.seal_semantic_native_preparation($1,$2,$3::jsonb)")
}

// Query is selected only by internal wrappers, never by request/config input.
func sealNativePreparationQueryInTransaction(ctx context.Context, tx pgx.Tx, tenant, run, workflow uuid.UUID, evidence json.RawMessage, query string) (v2input.JobTicket, error) {
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
	if err := tx.QueryRow(ctx, query, tenant, run, string(evidence)).Scan(&raw); err != nil {
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

// Explicit initial-orchestration admission. SQL seals the detector through the
// unchanged full evidence gate and enables its initial lease queue in the same
// transaction. Callers must commit before exposing even the private child ticket.
func (s SQLTransferSourceReader) sealAndEnableInitialDetectorInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, plan detectorDraftPlan, built v2input.Built) (v2input.JobTicket, error) {
	evidence, err := s.marshalDetectorAdmissionEvidence(ctx, who, plan, built)
	if err != nil {
		return v2input.JobTicket{}, err
	}
	return sealNativePreparationQueryInTransaction(ctx, tx, plan.request.TenantID, plan.shell.Run, plan.shell.WorkflowID, evidence, "SELECT photo.seal_and_enable_v27_native_initial_pipeline($1,$2,$3::jsonb)")
}
