package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// Full V27 catalog stability only, not required-model selection or qualification.
func (s SQLTransferSourceReader) qualificationPolicyInventory(ctx context.Context, who auth.Identity, tenantID uuid.UUID) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if s.DB == nil || tenantID == uuid.Nil || s.ProjectID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return "", v2input.ErrInvalid
	}
	var body []byte
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, "SELECT photo.semantic_proposal_qualification_policy_inventory($1,$2,$3,$4)", tenantID, s.ProjectID, who.UserID, who.SessionID).Scan(&body)
	})
	if err != nil {
		return "", err
	}
	var entries []json.RawMessage
	if len(body) > 4194304 || json.Unmarshal(body, &entries) != nil || len(entries) < 1 || len(entries) > 64 {
		return "", v2input.ErrInvalid
	}
	// PostgreSQL jsonb output is not JCS. Retain canonical bytes so the same
	// policy evidence/hash can be used by native admission and later replay.
	owned, err := canonical.Bytes(body)
	if err != nil {
		return "", v2input.ErrInvalid
	}
	return string(owned), nil
}

// Private owned original catalog and authorization scope.
type qualificationPolicySnapshot struct {
	tenant, project, actor, session uuid.UUID
	body                            string
}

func validQualificationPolicySnapshot(s SQLTransferSourceReader, who auth.Identity, tenantID uuid.UUID, p qualificationPolicySnapshot) bool {
	if tenantID == uuid.Nil || s.ProjectID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || p.tenant != tenantID || p.project != s.ProjectID || p.actor != who.UserID || p.session != who.SessionID || len(p.body) > 4194304 {
		return false
	}
	var entries []json.RawMessage
	return json.Unmarshal([]byte(p.body), &entries) == nil && len(entries) > 0 && len(entries) <= 64
}
func (s SQLTransferSourceReader) readQualificationPolicySnapshot(ctx context.Context, who auth.Identity, tenantID uuid.UUID) (qualificationPolicySnapshot, error) {
	body, err := s.qualificationPolicyInventory(ctx, who, tenantID)
	if err != nil {
		return qualificationPolicySnapshot{}, err
	}
	return qualificationPolicySnapshot{tenantID, s.ProjectID, who.UserID, who.SessionID, body}, nil
}
