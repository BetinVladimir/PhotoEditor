package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Caller commits only after this succeeds; rollback covers activation and outbox.
// Immutable publication must already be complete outside this transaction.
func (s SQLTransferSourceReader) sealDefectPreparationInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, prepared defectPersonAdmissionPreparation, built v2input.Built) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	evidence, err := s.marshalDefectAdmissionEvidence(ctx, who, prepared, built)
	if err != nil {
		return zero, err
	}
	plan := prepared.plan
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return zero, err
	}
	if role != "photo_scheduler" {
		return zero, v2input.ErrInvalid
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", plan.request.TenantID.String()); err != nil {
		return zero, err
	}
	var existing []byte
	if err := tx.QueryRow(ctx, "SELECT photo.v2_admission_ticket($1,$2,$3,$4)", plan.request.TenantID, who.UserID, plan.key, plan.request.RequestSHA).Scan(&existing); err != nil {
		return zero, err
	}
	if len(existing) == 0 {
		if err := s.lockRegisteredDefectPersonAdmission(ctx, tx, who, prepared, built); err != nil {
			return zero, err
		}
	}
	var raw []byte
	if err := tx.QueryRow(ctx, "SELECT photo.seal_semantic_defect_preparation($1,$2,$3::jsonb)", plan.request.TenantID, plan.shell.Run, string(evidence)).Scan(&raw); err != nil {
		return zero, err
	}
	var ticket v2input.JobTicket
	if len(raw) == 0 {
		return zero, v2input.ErrConflict
	}
	if json.Unmarshal(raw, &ticket) != nil || ticket.RunID != plan.shell.Run || ticket.WorkflowID != plan.shell.WorkflowID || ticket.State != "queued" {
		return zero, v2input.ErrInvalid
	}
	return ticket, nil
}
