package privacy

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
)

type OwnershipChoice struct {
	TenantID       uuid.UUID `json:"tenant_id"`
	TenantRevision string    `json:"tenant_revision"`
	AllowedActions []string  `json:"allowed_actions"`
}
type DeletionPreflight struct {
	PrivacyCaseID             uuid.UUID         `json:"privacy_case_id"`
	Revision                  string            `json:"revision"`
	OwnershipChoices          []OwnershipChoice `json:"ownership_choices"`
	RequiredRecentAuthPurpose string            `json:"required_recent_auth_purpose"`
}
type DeletionPreflightService interface {
	PrepareDeletion(context.Context, auth.Identity, string) (DeletionPreflight, error)
}

func (s PostgresPreferences) PrepareDeletion(ctx context.Context, who auth.Identity, key string) (DeletionPreflight, error) {
	if err := ctx.Err(); err != nil {
		return DeletionPreflight{}, err
	}
	if who.UserID == uuid.Nil || who.SessionID == uuid.Nil || who.Epoch < 0 {
		return DeletionPreflight{}, domain.NewError("FORBIDDEN", 403)
	}
	if !idempotency.ValidKey(key) {
		return DeletionPreflight{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	if s.DB == nil {
		return DeletionPreflight{}, domain.NewError("SERVICE_UNAVAILABLE", 503)
	}
	var out DeletionPreflight
	var revision int64
	var choices []byte
	err := s.DB.QueryRow(ctx, `SELECT privacy_case_id,revision,ownership_choices,required_recent_auth_purpose FROM photo.account_prepare_deletion($1,$2,$3,$4)`, who.UserID, who.SessionID, who.Epoch, key).Scan(&out.PrivacyCaseID, &revision, &choices, &out.RequiredRecentAuthPurpose)
	if err != nil {
		if ctx.Err() != nil {
			return DeletionPreflight{}, ctx.Err()
		}
		var pg *pgconn.PgError
		if errors.As(err, &pg) {
			switch pg.Message {
			case "BILLING_AUTHORITY_INVALID":
				return DeletionPreflight{}, domain.NewError("FORBIDDEN", 403)
			case "DELETION_PREFLIGHT_STALE":
				return DeletionPreflight{}, domain.NewError("REVISION_CONFLICT", 409)
			case "DELETION_PREFLIGHT_INPUT_INVALID":
				return DeletionPreflight{}, domain.NewError("VALIDATION_FAILED", 422)
			}
		}
		return DeletionPreflight{}, domain.NewError("SERVICE_UNAVAILABLE", 503)
	}
	if revision < 1 || out.PrivacyCaseID == uuid.Nil || out.RequiredRecentAuthPurpose != "delete_profile" || json.Unmarshal(choices, &out.OwnershipChoices) != nil || out.OwnershipChoices == nil {
		return DeletionPreflight{}, domain.NewError("SERVICE_UNAVAILABLE", 503)
	}
	out.Revision = strconv.FormatInt(revision, 10)
	return out, nil
}
