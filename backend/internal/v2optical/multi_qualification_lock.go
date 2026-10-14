package v2optical

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"time"
)

// LockMultiQualification holds policy/report/calibration locks through the
// caller transaction. Exact evidence bytes must be verified independently;
// this relational gate alone neither qualifies runtime nor activates a task.
func LockMultiQualification(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant, project uuid.UUID, pin MultiQualificationPin, operation string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	if (operation != "optical.panorama" && operation != "optical.focus_stack") || tx == nil || tenant == uuid.Nil || project == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return v2input.ErrInvalid
	}
	body, err := pin.Compile()
	if err != nil {
		return err
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return err
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_v29_multi_qualification($1,$2,$3,$4,$5::jsonb,$6)", tenant, project, who.UserID, who.SessionID, string(body), operation).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrExpired
	}
	return ctx.Err()
}
