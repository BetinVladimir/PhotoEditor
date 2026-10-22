package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

var ErrAuthoredSegmentPending = errors.New("authored segment pending")

// ResumeAdmission never re-stages a sealed/adopted child. An adopted return is
// only a locator for independent physical readback; it cannot append a phase,
// complete a producer, clear review_required or settle the parent.
func (s SQLAuthoredSegmentPreparation) ResumeAdmission(ctx context.Context, who auth.Identity, tenant, parent uuid.UUID, ordinal int, currentLayers, protected *v2input.Ref, objects artifactjson.Objects) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if s.Params.Scheduler == nil || s.API == nil || objects == nil || tenant == uuid.Nil || parent == uuid.Nil || ordinal < 1 || ordinal > 4998 {
		return "", v2input.ErrInvalid
	}
	source, present, err := ReadAuthoredInstructionSource(ctx, s.Params.Scheduler, tenant, parent, ordinal)
	if err != nil {
		return "", err
	}
	if !present {
		return "", v2input.ErrExpired
	}
	if source.Actor != who.UserID || source.Session != who.SessionID {
		return "", v2input.ErrInvalid
	}
	var state *string
	err = database.TenantTransaction(ctx, s.Params.Scheduler, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT photo.authored_render_segment_preparation_state($1,$2,$3)`, tenant, parent, ordinal).Scan(&state)
	})
	if err != nil {
		return "", err
	}
	if state == nil {
		return "", v2input.ErrExpired
	}
	switch *state {
	case "pending":
		return "pending", ErrAuthoredSegmentPending
	case "adopted":
		return "adopted", ctx.Err()
	case "absent", "draft":
		plan, err := s.Prepare(ctx, who, tenant, parent, ordinal, currentLayers, protected)
		if err != nil {
			return "", err
		}
		admission := SQLAuthoredSegmentAdmission{Scheduler: s.Params.Scheduler}
		if err = admission.Admit(ctx, tenant, parent, ordinal, plan, s.Params.Bucket, objects); err != nil {
			return "", err
		}
		return "pending", ErrAuthoredSegmentPending
	default:
		return "", v2input.ErrInvalid
	}
}
