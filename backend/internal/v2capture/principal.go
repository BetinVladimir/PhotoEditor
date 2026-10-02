package v2capture

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"photoeditor/backend/internal/domain"
)

type DevicePrincipal struct {
	TenantID        uuid.UUID
	ProjectID       uuid.UUID
	DeviceID        uuid.UUID
	CredentialEpoch int64
}

// AuthenticateDevice is an HTTP preflight. Each device mutation must also
// recheck this active device/epoch under its own database transaction and
// lock, so revocation cannot race a successful write.
func (s PublicService) AuthenticateDevice(ctx context.Context,
	authorization string, tenantID uuid.UUID) (DevicePrincipal, error) {
	var principal DevicePrincipal
	if !strings.HasPrefix(authorization, "Bearer ") ||
		strings.ContainsAny(authorization[7:], " \t\r\n") {
		return principal, domain.NewError("INVALID_CREDENTIALS", 401)
	}
	deviceID, claims, err := s.Credentials.VerifyToken(authorization[7:])
	if err != nil || claims.TenantID != tenantID {
		return principal, domain.NewError("INVALID_CREDENTIALS", 401)
	}
	if s.DB == nil {
		return principal, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	var active bool
	err = s.DB.QueryRow(ctx, `SELECT photo.capture_device_principal($1,$2,$3,$4)`,
		claims.TenantID, claims.ProjectID, deviceID, claims.Epoch).Scan(&active)
	if err != nil {
		return principal, mutationError(err)
	}
	if !active {
		return principal, domain.NewError("INVALID_CREDENTIALS", 401)
	}
	return DevicePrincipal{TenantID: claims.TenantID, ProjectID: claims.ProjectID,
		DeviceID: deviceID, CredentialEpoch: claims.Epoch}, nil
}
