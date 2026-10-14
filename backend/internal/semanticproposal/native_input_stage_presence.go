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

type nativeStagePresence string

const (
	nativeStageAbsent   nativeStagePresence = "absent"
	nativeStagePresent  nativeStagePresence = "present"
	nativeStageConflict nativeStagePresence = "conflict"
)

func decodeNativeStagePresence(value string) (nativeStagePresence, error) {
	switch nativeStagePresence(value) {
	case nativeStageAbsent, nativeStagePresent, nativeStageConflict:
		return nativeStagePresence(value), nil
	}
	return "", v2input.ErrConflict
}

// Presence is a scheduling hint under the exact original detector config. It
// grants no creation/admission authority. Callers must retain the parent lock and
// use complete begin/register/seal gates, which recheck concurrent changes.
func (s SQLTransferSourceReader) readNativeStagePresence(ctx context.Context, who auth.Identity, detector detectorDraftPlan, operation string) (nativeStagePresence, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if operation != "mask.person" && operation != "face.landmarks" && operation != "face.parse" && operation != "mask.face_skin" && operation != "defects.propose" {
		return "", v2input.ErrInvalid
	}
	if s.DB == nil || s.ProjectID != detector.shell.Project {
		return "", v2input.ErrInvalid
	}
	if err := verifyDetectorPlanSeal(ctx, detector); err != nil {
		return "", err
	}
	var value *string
	err := tenant.WithTenant(ctx, s.DB, who, detector.request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, "SELECT photo.semantic_native_input_stage_presence($1,$2,$3,$4,$5,$6,$7)", detector.request.TenantID, s.ProjectID, who.UserID, who.SessionID, detector.request.RunID, detector.shell.ConfigSHA, operation).Scan(&value)
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
