package v2capture

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
)

type ConfirmFrameRequest struct {
	AlertID uuid.UUID `json:"alert_id"`
}
type FrameConfirmation struct {
	ConfirmationID uuid.UUID `json:"confirmation_id"`
	ReceiptID      uuid.UUID `json:"receipt_id"`
	AlertID        uuid.UUID `json:"alert_id"`
}

func (s Service) ConfirmFrame(ctx context.Context, who auth.Identity, tenantID, receiptID uuid.UUID, key string, request ConfirmFrameRequest) (idempotency.Response, error) {
	if s.DB == nil {
		return idempotency.Response{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if receiptID == uuid.Nil || request.AlertID == uuid.Nil {
		return idempotency.Response{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	if !idempotency.ValidKey(key) {
		return idempotency.Response{}, domain.NewError("IDEMPOTENCY_KEY_REQUIRED", 400)
	}
	var out idempotency.Response
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		var projectID uuid.UUID
		e := tx.QueryRow(ctx, `SELECT project_id FROM photo.capture_receipts WHERE tenant_id=$1 AND id=$2`, tenantID, receiptID).Scan(&projectID)
		if e == pgx.ErrNoRows {
			return domain.NewError("NOT_FOUND", 404)
		}
		if e != nil {
			return e
		}
		if _, e = project.Authorize(ctx, tx, tenantID, projectID, who.UserID, role, domain.Editor, false); e != nil {
			return e
		}
		out, e = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tenantID, Principal: who.UserID, PrincipalType: "user", Route: "Capture.ConfirmFrame/" + receiptID.String(), Key: key}, request, time.Now().Add(30*24*time.Hour), func() (idempotency.Response, error) {
			var id uuid.UUID
			e := tx.QueryRow(ctx, `SELECT photo.confirm_capture_frame($1,$2,$3,$4,$5)`, tenantID, receiptID, request.AlertID, who.UserID, who.SessionID).Scan(&id)
			if e != nil {
				return idempotency.Response{}, e
			}
			body, e := json.Marshal(FrameConfirmation{id, receiptID, request.AlertID})
			return idempotency.Response{Status: 200, Body: body, ResourceID: &id}, e
		})
		return e
	})
	return out, mutationError(err)
}
