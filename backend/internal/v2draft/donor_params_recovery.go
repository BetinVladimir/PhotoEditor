package v2draft

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"time"
)

// ReadIntent returns owned canonical recovery data after commit and live SQL
// authority checks. It is a seed for reloading original evidence; callers must
// recompile qualified inputs and use Publish, whose exact intent replay rejects
// any changed parameters or qualification. It is not a publication/execution grant.
func (s SQLDonorParamsPublisher) ReadIntent(ctx context.Context, who auth.Identity, tenant, run uuid.UUID) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.DB == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tenant == uuid.Nil || run == uuid.Nil {
		return nil, v2input.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, v2input.ErrInvalid
	}
	defer rollbackDonorRecovery(ctx, tx)
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return nil, err
	}
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return nil, err
	}
	if role != "photo_scheduler" {
		return nil, v2input.ErrInvalid
	}
	var raw []byte
	if err = tx.QueryRow(ctx, "SELECT photo.read_v28_donor_params_intent($1,$2,$3,$4)", tenant, run, who.UserID, who.SessionID).Scan(&raw); err != nil {
		return nil, err
	}
	if len(raw) < 2 || len(raw) > 1<<20 {
		return nil, v2input.ErrExpired
	}
	body, err := canonical.Bytes(raw)
	if err != nil || len(body) < 2 || body[0] != '{' {
		return nil, v2input.ErrInvalid
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return append([]byte(nil), body...), nil
}

// ReadRecoverySeed combines committed SQL recovery data with exact typed
// workflow/actor binding. Original catalog evidence must still be reloaded.
func (s SQLDonorParamsPublisher) ReadRecoverySeed(ctx context.Context, who auth.Identity, tenant, run, workflow uuid.UUID) (DonorParamsRecoverySeed, error) {
	if workflow == uuid.Nil {
		return DonorParamsRecoverySeed{}, v2input.ErrInvalid
	}
	raw, err := s.ReadIntent(ctx, who, tenant, run)
	if err != nil {
		return DonorParamsRecoverySeed{}, err
	}
	return ParseDonorParamsRecoveryIntent(ctx, who, workflow, raw)
}
