package v2capture

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/domain"
)

// AdmitDeviceTx fences every device mutation against revocation. The caller
// must establish app.tenant_id from a verified capture token before entering
// this transaction and keep it open through the mutation. FOR SHARE holds a
// row lock until commit, while Revoke takes FOR UPDATE on the device row.
func AdmitDeviceTx(ctx context.Context, tx pgx.Tx, principal DevicePrincipal) error {
	if principal.TenantID == uuid.Nil || principal.ProjectID == uuid.Nil ||
		principal.DeviceID == uuid.Nil || principal.CredentialEpoch < 1 {
		return domain.NewError("INVALID_CREDENTIALS", 401)
	}
	var active bool
	err := tx.QueryRow(ctx, `SELECT photo.capture_device_admit_tx($1,$2,$3,$4)`, principal.TenantID, principal.ProjectID,
		principal.DeviceID, principal.CredentialEpoch).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !active) {
		return domain.NewError("INVALID_CREDENTIALS", 401)
	}
	return err
}
