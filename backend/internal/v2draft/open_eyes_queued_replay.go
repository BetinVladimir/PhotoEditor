package v2draft

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type SQLOpenEyesQueuedReplay struct{ DB *pgxpool.Pool }

// Read requires the same authored intent, actor, session and deterministic
// run before the existing current-authority donor receipt reader is invoked.
func (s SQLOpenEyesQueuedReplay) Read(ctx context.Context, who auth.Identity,
	tenant uuid.UUID, key string, intent OpenEyesClientIntent) (DonorQueuedTicket, error) {
	var zero DonorQueuedTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tenant == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	run, workflow, err := VerifyOpenEyesIntentOwner(intent, tenant, who.UserID, key)
	if err != nil {
		return zero, err
	}
	digest, err := canonical.Hash(intent)
	if err != nil {
		return zero, err
	}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.DB.Begin(bounded)
	if err != nil {
		return zero, err
	}
	defer rollbackDonorRecovery(bounded, tx)
	if _, err = tx.Exec(bounded, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return zero, err
	}
	var role string
	if err = tx.QueryRow(bounded, "SELECT current_user").Scan(&role); err != nil {
		return zero, err
	}
	if role != "photo_scheduler" {
		return zero, v2input.ErrInvalid
	}
	var matches bool
	if err = tx.QueryRow(bounded, `SELECT photo.v28_open_eyes_queued_request_matches($1,$2,$3,$4,$5)`,
		tenant, run, who.UserID, who.SessionID, digest).Scan(&matches); err != nil {
		return zero, err
	}
	if !matches {
		return zero, v2input.ErrExpired
	}
	if err = tx.Commit(bounded); err != nil {
		return zero, err
	}
	return (SQLDonorDescriptorStore{DB: s.DB}).ReadQueuedTicketForWorkflow(ctx,
		who, tenant, run, workflow, intent.Ordinal)
}
