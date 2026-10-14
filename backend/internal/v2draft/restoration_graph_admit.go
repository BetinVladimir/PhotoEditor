package v2draft

import (
	"context"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// AdmitRegistered seals the already registered one-task restoration graph.
// SQL repeats live origin, qualification, consent, quota, closure and generic
// graph gates under one scheduler transaction. No object IO occurs here.
func (s SQLRestorationDescriptorStore) AdmitRegistered(ctx context.Context,
	who auth.Identity, tenant, run uuid.UUID,
	prepared RestorationResolvedDraft, paramsRaw []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil || tenant == uuid.Nil || run == uuid.Nil ||
		who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return v2input.ErrInvalid
	}
	descriptor, err := prepared.Descriptor(paramsRaw)
	if err != nil {
		return err
	}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.DB.Begin(bounded)
	if err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	defer rollbackDonorRecovery(bounded, tx)
	var role string
	if err = tx.QueryRow(bounded, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	if _, err = tx.Exec(bounded, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return err
	}
	var ready bool
	if err = tx.QueryRow(bounded, "SELECT photo.stage_v28_restoration_task($1,$2,$3,$4,$5,$6)",
		tenant, run, who.UserID, who.SessionID, string(descriptor), string(paramsRaw)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrExpired
	}
	if err = tx.QueryRow(bounded, "SELECT photo.seal_v28_restoration_graph($1,$2,$3,$4)",
		tenant, run, who.UserID, who.SessionID).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrExpired
	}
	if err = bounded.Err(); err != nil {
		return err
	}
	return tx.Commit(bounded)
}
