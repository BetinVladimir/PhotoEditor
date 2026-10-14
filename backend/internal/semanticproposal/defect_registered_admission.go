package semanticproposal

import (
	"context"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Locks the actual person-qualified preparation and verifies the complete
// already registered transport in the same transaction. A missing or changed
// registry is a conflict, never an invitation to register during activation.
// Caller owns rollback; this function itself does not seal or queue the graph.
func (s SQLTransferSourceReader) lockRegisteredDefectPersonAdmission(ctx context.Context, tx pgx.Tx, who auth.Identity, prepared defectPersonAdmissionPreparation, built v2input.Built) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	binding, err := marshalDefectRegistrationBinding(ctx, prepared.plan, built)
	if err != nil {
		return err
	}
	evidence, err := s.marshalDefectAdmissionEvidence(ctx, who, prepared, built)
	if err != nil {
		return err
	}
	if err := s.lockDefectPersonAdmissionInTransaction(ctx, tx, who, prepared); err != nil {
		return err
	}
	request := prepared.plan.request
	request.RunID = prepared.plan.shell.Run
	var state string
	err = tx.QueryRow(ctx, "SELECT photo.semantic_defect_registration_binding_state($1,$2,$3::jsonb)", request.TenantID, request.RunID, string(binding)).Scan(&state)
	if err != nil {
		return err
	}
	if state != "replayed" {
		return v2input.ErrConflict
	}
	// Recheck live actor/session and complete policy after the final registry read.
	if err := s.lockDefectPolicyExecutionInTransaction(ctx, tx, who, request.TenantID, request.Intent.Reference, prepared.plan.execution); err != nil {
		return err
	}
	var matches bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_defect_admission_evidence($1,$2,$3::jsonb)", request.TenantID, request.RunID, string(evidence)).Scan(&matches); err != nil {
		return err
	}
	if !matches {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
