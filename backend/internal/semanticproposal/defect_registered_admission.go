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
		return defectAdmissionStageError(defectStageRegisteredContext, err)
	}
	if tx == nil {
		return defectAdmissionStageError(defectStageRegisteredTransaction, v2input.ErrInvalid)
	}
	binding, err := marshalDefectRegistrationBinding(ctx, prepared.plan, built)
	if err != nil {
		return defectAdmissionStageError(defectStageRegistrationPacket, err)
	}
	evidence, err := s.marshalDefectAdmissionEvidence(ctx, who, prepared, built)
	if err != nil {
		return defectAdmissionStageError(defectStageAdmissionPacket, err)
	}
	if err := s.lockDefectPersonAdmissionInTransaction(ctx, tx, who, prepared); err != nil {
		return defectAdmissionStageError(defectStagePersonAuthority, err)
	}
	request := prepared.plan.request
	request.RunID = prepared.plan.shell.Run
	var state string
	err = tx.QueryRow(ctx, "SELECT photo.semantic_defect_registration_binding_state($1,$2,$3::jsonb)", request.TenantID, request.RunID, string(binding)).Scan(&state)
	if err != nil {
		return defectAdmissionStageError(defectStageRegistrationQuery, err)
	}
	if state != "replayed" {
		return defectAdmissionStageError(defectStageRegistrationState, v2input.ErrConflict)
	}
	// Recheck live actor/session and complete policy after the final registry read.
	if err := s.lockDefectPolicyExecutionInTransaction(ctx, tx, who, request.TenantID, request.Intent.Reference, prepared.plan.execution); err != nil {
		return defectAdmissionStageError(defectStageRegisteredPolicy, err)
	}
	var matches bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_defect_admission_evidence($1,$2,$3::jsonb)", request.TenantID, request.RunID, string(evidence)).Scan(&matches); err != nil {
		return defectAdmissionStageError(defectStageEvidenceQuery, err)
	}
	if !matches {
		return defectAdmissionStageError(defectStageEvidenceState, defectAdmissionClosedDiagnostic(ctx, tx, request.TenantID, request.RunID, string(evidence), v2input.ErrConflict))
	}
	return defectAdmissionStageError(defectStageRegisteredCompletion, ctx.Err())
}
