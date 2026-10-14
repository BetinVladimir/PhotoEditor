package v2draft

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
	"time"
)

type SQLDonorDescriptorStore struct {
	DB v2portrait.DonorConfirmationTransactions
}

// Read returns owned committed recovery bytes. RestoreQualifiedDonorTaskDescriptor
// and final live admission are still required before reusing a stored graph.
func (s SQLDonorDescriptorStore) Read(ctx context.Context, who auth.Identity, tenant, run uuid.UUID) ([]byte, error) {
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
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return nil, err
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return nil, err
	}
	if role != "photo_scheduler" {
		return nil, v2input.ErrInvalid
	}
	var raw []byte
	if err := tx.QueryRow(ctx, "SELECT photo.read_v28_donor_descriptor($1,$2,$3,$4)", tenant, run, who.UserID, who.SessionID).Scan(&raw); err != nil {
		return nil, err
	}
	if len(raw) < 2 || len(raw) > 1<<20 {
		return nil, v2input.ErrExpired
	}
	body, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(body, raw) {
		return nil, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return append([]byte(nil), body...), nil
}

// Save persists qualified recovery data and commits before returning. It does
// not create a task, register transport or seal a graph; SQL repeats live scope.
func (s SQLDonorDescriptorStore) Save(ctx context.Context, who auth.Identity, tenant, run uuid.UUID, frozen FrozenDonorTask) error {
	return s.write(ctx, who, tenant, run, frozen, false)
}

// Stage atomically retains the descriptor and registers a blocked task.
// It does not register transport or activate execution.
func (s SQLDonorDescriptorStore) Stage(ctx context.Context, who auth.Identity, tenant, run uuid.UUID, frozen FrozenDonorTask) error {
	return s.write(ctx, who, tenant, run, frozen, true)
}

func (s SQLDonorDescriptorStore) write(ctx context.Context, who auth.Identity, tenant, run uuid.UUID, frozen FrozenDonorTask, stage bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tenant == uuid.Nil || run == uuid.Nil || frozen.qualification == nil {
		return v2input.ErrInvalid
	}
	if _, err := frozen.qualification.Qualification(); err != nil {
		return err
	}
	body, err := frozen.Descriptor()
	if err != nil {
		return err
	}
	if len(body) > 1<<20 || len(frozen.params) < 2 || len(frozen.params) > 65536 {
		return v2input.ErrInvalid
	}
	params := append([]byte(nil), frozen.params...)
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	defer rollbackDonorRecovery(ctx, tx)
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return err
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	var ready bool
	query := "SELECT photo.save_v28_donor_descriptor($1,$2,$3,$4,$5,$6)"
	if stage {
		query = "SELECT photo.stage_v28_donor_task($1,$2,$3,$4,$5,$6)"
	}
	if err := tx.QueryRow(ctx, query, tenant, run, who.UserID, who.SessionID, string(body), string(params)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrExpired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
