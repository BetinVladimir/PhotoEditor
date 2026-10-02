package v2capture

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/pagination"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
)

type Service struct {
	DB      *pgxpool.Pool
	Cursors pagination.Codec
}

type RegisterRequest struct {
	ProjectID uuid.UUID `json:"project_id"`
	Label     string    `json:"label"`
	PublicKey string    `json:"public_key"`
}

type RevokeRequest struct {
	ExpectedRevision string `json:"expected_revision"`
}

func validPublicKey(encoded string) bool {
	if len(encoded) != 43 {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	return err == nil && len(decoded) == ed25519.PublicKeySize &&
		base64.RawURLEncoding.EncodeToString(decoded) == encoded
}

func validLabel(label string) bool {
	if !utf8.ValidString(label) || len([]rune(label)) < 1 || len([]rune(label)) > 120 {
		return false
	}
	for _, r := range label {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

func mutationError(err error) error {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return err
	}
	if pg.Message == "V24_IDENTITY_CONSENT_REVOKED" {
		return domain.NewError("CONSENT_REVOKED", 409)
	}
	switch pg.Code {
	case "40001", "23505":
		return domain.NewError("REVISION_CONFLICT", 409)
	case "42501":
		return domain.NewError("FORBIDDEN", 403)
	case "P0002":
		return domain.NewError("NOT_FOUND", 404)
	case "22023":
		return domain.NewError("VALIDATION_FAILED", 422)
	case "23514":
		return domain.NewError("CAPTURE_EVIDENCE_INVALID", 409)
	case "55000":
		return domain.NewError("UPLOAD_STATE_INVALID", 409)
	case "42P01", "42883":
		return domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	return err
}

func (s Service) Register(ctx context.Context, who auth.Identity,
	tenantID uuid.UUID, key string, req RegisterRequest) (idempotency.Response, error) {
	if s.DB == nil {
		return idempotency.Response{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if req.ProjectID == uuid.Nil || !validLabel(req.Label) || !validPublicKey(req.PublicKey) {
		return idempotency.Response{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	if !idempotency.ValidKey(key) {
		return idempotency.Response{}, domain.NewError("IDEMPOTENCY_KEY_REQUIRED", 400)
	}
	var out idempotency.Response
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Admin,
		func(tx pgx.Tx, role domain.Role) error {
			if _, e := project.Authorize(ctx, tx, tenantID, req.ProjectID,
				who.UserID, role, domain.Admin, false); e != nil {
				return e
			}
			var e error
			out, e = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tenantID,
				Principal: who.UserID, PrincipalType: "user", Route: "Capture.RegisterDevice",
				Key: key}, req, time.Now().Add(24*time.Hour),
				func() (idempotency.Response, error) {
					var body json.RawMessage
					e := tx.QueryRow(ctx, `SELECT photo.register_capture_device($1,$2,$3,$4,$5,$6)`,
						tenantID, req.ProjectID, req.Label, req.PublicKey,
						who.UserID, who.SessionID).Scan(&body)
					return idempotency.Response{Status: 201, Body: body}, e
				})
			return e
		})
	return out, mutationError(err)
}

func (s Service) Revoke(ctx context.Context, who auth.Identity,
	tenantID, deviceID uuid.UUID, key string,
	req RevokeRequest) (idempotency.Response, error) {
	if s.DB == nil {
		return idempotency.Response{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if deviceID == uuid.Nil || req.ExpectedRevision == "" ||
		len(req.ExpectedRevision) > 19 || req.ExpectedRevision[0] == '0' {
		return idempotency.Response{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	revision, err := strconv.ParseInt(req.ExpectedRevision, 10, 64)
	if err != nil || revision < 1 {
		return idempotency.Response{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	if !idempotency.ValidKey(key) {
		return idempotency.Response{}, domain.NewError("IDEMPOTENCY_KEY_REQUIRED", 400)
	}
	var out idempotency.Response
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Admin,
		func(tx pgx.Tx, _ domain.Role) error {
			var e error
			out, e = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tenantID,
				Principal: who.UserID, PrincipalType: "user",
				Route: "Capture.RevokeDevice." + deviceID.String(), Key: key},
				req, time.Now().Add(24*time.Hour),
				func() (idempotency.Response, error) {
					var body json.RawMessage
					e := tx.QueryRow(ctx, `SELECT photo.revoke_capture_device($1,$2,$3,$4,$5)`,
						tenantID, deviceID, revision, who.UserID, who.SessionID).Scan(&body)
					return idempotency.Response{Status: 200, Body: body}, e
				})
			return e
		})
	return out, mutationError(err)
}
