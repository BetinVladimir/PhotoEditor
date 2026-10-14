package v2draft

import (
	"bytes"
	"context"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

type SQLRestorationDescriptorStore struct {
	DB v2portrait.DonorConfirmationTransactions
}

// Save stores immutable recovery bytes after the SQL function replays current
// original mask production, tenant ownership and qualification. No task starts.
func (s SQLRestorationDescriptorStore) Save(ctx context.Context, who auth.Identity,
	tenant, run uuid.UUID, prepared RestorationResolvedDraft, paramsRaw []byte) error {
	return s.write(ctx, who, tenant, run, prepared, paramsRaw, false)
}

// Stage atomically saves the recovery descriptor and a blocked task. It does
// not register transport, seal a graph or make the task executable.
func (s SQLRestorationDescriptorStore) Stage(ctx context.Context, who auth.Identity,
	tenant, run uuid.UUID, prepared RestorationResolvedDraft, paramsRaw []byte) error {
	return s.write(ctx, who, tenant, run, prepared, paramsRaw, true)
}

func (s SQLRestorationDescriptorStore) write(ctx context.Context, who auth.Identity,
	tenant, run uuid.UUID, prepared RestorationResolvedDraft, paramsRaw []byte, stage bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil || tenant == uuid.Nil || run == uuid.Nil ||
		who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return v2input.ErrInvalid
	}
	body, err := prepared.Descriptor(paramsRaw)
	if err != nil || len(body) > 1<<20 {
		return v2input.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	defer rollbackDonorRecovery(ctx, tx)
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return err
	}
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	var accepted bool
	query := "SELECT photo.save_v28_restoration_descriptor($1,$2,$3,$4,$5,$6)"
	if stage {
		query = "SELECT photo.stage_v28_restoration_task($1,$2,$3,$4,$5,$6)"
	}
	if err = tx.QueryRow(ctx, query,
		tenant, run, who.UserID, who.SessionID, string(body), string(paramsRaw)).Scan(&accepted); err != nil {
		return err
	}
	if !accepted {
		return v2input.ErrExpired
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Read obtains recovery bytes only. The caller must use
// RestoreRestorationTaskDescriptor before reuse or graph publication.
func (s SQLRestorationDescriptorStore) Read(ctx context.Context, who auth.Identity,
	tenant, run uuid.UUID) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.DB == nil || tenant == uuid.Nil || run == uuid.Nil ||
		who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
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
	if err = tx.QueryRow(ctx, "SELECT photo.read_v28_restoration_descriptor($1,$2,$3,$4)",
		tenant, run, who.UserID, who.SessionID).Scan(&raw); err != nil {
		return nil, err
	}
	if len(raw) < 2 || len(raw) > 1<<20 {
		return nil, v2input.ErrExpired
	}
	body, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(body, raw) {
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
