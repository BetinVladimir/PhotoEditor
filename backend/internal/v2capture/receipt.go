package v2capture

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
)

type ReceiptRequest struct {
	DeviceID   uuid.UUID  `json:"device_id"`
	Sequence   string     `json:"sequence"`
	ContentSHA string     `json:"content_sha"`
	UploadID   uuid.UUID  `json:"upload_id"`
	CapturedAt *time.Time `json:"captured_at"`
}

func validateReceipt(principal DevicePrincipal, sessionID uuid.UUID, request ReceiptRequest) (int64, error) {
	if sessionID == uuid.Nil || request.UploadID == uuid.Nil || request.DeviceID != principal.DeviceID || request.DeviceID == uuid.Nil || len(request.Sequence) < 1 || len(request.Sequence) > 19 || (len(request.Sequence) > 1 && request.Sequence[0] == '0') || len(request.ContentSHA) != 64 {
		return 0, domain.NewError("VALIDATION_FAILED", 422)
	}
	for _, c := range request.Sequence {
		if c < '0' || c > '9' {
			return 0, domain.NewError("VALIDATION_FAILED", 422)
		}
	}
	sequence, err := strconv.ParseInt(request.Sequence, 10, 64)
	if err != nil || sequence < 0 {
		return 0, domain.NewError("VALIDATION_FAILED", 422)
	}
	for _, c := range request.ContentSHA {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return 0, domain.NewError("VALIDATION_FAILED", 422)
		}
	}
	return sequence, nil
}

func (s PublicService) AcceptReceipt(ctx context.Context, principal DevicePrincipal,
	sessionID uuid.UUID, key string, request ReceiptRequest) (idempotency.Response, error) {
	sequence, err := validateReceipt(principal, sessionID, request)
	if err != nil {
		return idempotency.Response{}, err
	}
	if s.DB == nil {
		return idempotency.Response{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	var out idempotency.Response
	err = database.TenantTransaction(ctx, s.DB, principal.TenantID, func(tx pgx.Tx) error {
		var e error
		out, e = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: principal.TenantID, Principal: principal.DeviceID,
			PrincipalType: "device", Route: "Capture.AcceptReceipt/" + sessionID.String(), Key: key}, request,
			time.Now().Add(30*24*time.Hour), func() (idempotency.Response, error) {
				var body json.RawMessage
				e := tx.QueryRow(ctx, `SELECT photo.accept_capture_receipt($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
					principal.TenantID, principal.ProjectID, principal.DeviceID, principal.CredentialEpoch,
					sessionID, sequence, request.ContentSHA, request.UploadID, request.CapturedAt).Scan(&body)
				return idempotency.Response{Status: 201, Body: body}, e
			})
		if e != nil {
			return e
		}
		// Cached HTTP responses do not bypass device revocation.
		return AdmitDeviceTx(ctx, tx, principal)
	})
	return out, mutationError(err)
}
