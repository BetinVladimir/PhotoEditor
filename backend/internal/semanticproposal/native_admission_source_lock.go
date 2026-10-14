package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Caller owns the scheduler transaction/tenant setting and rolls back all
// failures. These locks do not replace anchor, registration, quota or sealing.
func (s SQLTransferSourceReader) lockDetectorAdmissionSourceInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, plan detectorDraftPlan, built v2input.Built) error {
	evidence, err := s.marshalDetectorAdmissionEvidence(ctx, who, plan, built)
	if err != nil {
		return err
	}
	return s.lockNativeAdmissionSourceInTransaction(ctx, tx, who, plan.request.TenantID, evidence)
}
func (s SQLTransferSourceReader) lockPersonAdmissionSourceInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, plan personDraftPlan, built v2input.Built) error {
	evidence, err := s.marshalPersonAdmissionEvidence(ctx, who, plan, built)
	if err != nil {
		return err
	}
	return s.lockNativeAdmissionSourceInTransaction(ctx, tx, who, plan.request.TenantID, evidence)
}

func (s SQLTransferSourceReader) lockNativeAdmissionSourceInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, evidence json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || tenant == uuid.Nil || s.ProjectID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return v2input.ErrInvalid
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_native_source_authority($1,$2,$3,$4,$5::jsonb)", tenant, s.ProjectID, who.UserID, who.SessionID, string(evidence)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
