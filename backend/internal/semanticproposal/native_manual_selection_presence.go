package semanticproposal

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// Absence is a scheduling hint only under the authenticated original detector.
// Present selection still requires the full checkpoint codec and physical IO.
func (s SQLTransferSourceReader) readNativeManualSelectionPresence(ctx context.Context, who auth.Identity, detector detectorDraftPlan) (nativeStagePresence, error) {
	if err := verifyDetectorPlanSeal(ctx, detector); err != nil {
		return "", err
	}
	if s.DB == nil || s.ProjectID != detector.shell.Project {
		return "", v2input.ErrInvalid
	}
	var value *string
	err := tenant.WithTenant(ctx, s.DB, who, detector.request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, "SELECT photo.v27_native_manual_selection_presence($1,$2,$3,$4,$5,$6)", detector.request.TenantID, s.ProjectID, who.UserID, who.SessionID, detector.request.RunID, detector.shell.ConfigSHA).Scan(&value)
	})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && value == nil) {
		return "", v2input.ErrConflict
	}
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return decodeNativeStagePresence(*value)
}

func (s SQLTransferSourceReader) readNativeManualChildTerminal(ctx context.Context, who auth.Identity, detector detectorDraftPlan) (bool, error) {
	if err := verifyDetectorPlanSeal(ctx, detector); err != nil {
		return false, err
	}
	if s.DB == nil || s.ProjectID != detector.shell.Project {
		return false, v2input.ErrInvalid
	}
	var value *bool
	err := tenant.WithTenant(ctx, s.DB, who, detector.request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, "SELECT photo.v27_native_manual_child_terminal($1,$2,$3,$4,$5,$6)", detector.request.TenantID, s.ProjectID, who.UserID, who.SessionID, detector.request.RunID, detector.shell.ConfigSHA).Scan(&value)
	})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && value == nil) {
		return false, v2input.ErrConflict
	}
	if err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return *value, nil
}
