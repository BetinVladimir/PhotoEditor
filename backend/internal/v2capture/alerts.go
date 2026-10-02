package v2capture

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
)

func (s Service) Acknowledge(ctx context.Context, who auth.Identity, tenantID, alertID uuid.UUID,
	key string, request RevokeRequest) (idempotency.Response, error) {
	revision, err := strconv.ParseInt(request.ExpectedRevision, 10, 64)
	if err != nil || revision < 1 || strconv.FormatInt(revision, 10) != request.ExpectedRevision || alertID == uuid.Nil {
		return idempotency.Response{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	if s.DB == nil {
		return idempotency.Response{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	var out idempotency.Response
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		var projectID uuid.UUID
		e := tx.QueryRow(ctx, `SELECT project_id FROM photo.capture_alerts WHERE tenant_id=$1 AND id=$2`, tenantID, alertID).Scan(&projectID)
		if e == pgx.ErrNoRows {
			return domain.NewError("NOT_FOUND", 404)
		}
		if e != nil {
			return e
		}
		if _, e = project.Authorize(ctx, tx, tenantID, projectID, who.UserID, role, domain.Editor, false); e != nil {
			return e
		}
		out, e = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tenantID, Principal: who.UserID, PrincipalType: "user",
			Route: "Capture.Acknowledge/" + alertID.String(), Key: key}, request, time.Now().Add(30*24*time.Hour), func() (idempotency.Response, error) {
			var body json.RawMessage
			e := tx.QueryRow(ctx, `SELECT photo.acknowledge_capture_alert($1,$2,$3,$4,$5)`, tenantID, alertID, revision, who.UserID, who.SessionID).Scan(&body)
			return idempotency.Response{Status: 200, Body: body}, e
		})
		return e
	})
	return out, mutationError(err)
}
