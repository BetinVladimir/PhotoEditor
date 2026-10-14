package v2draft

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"time"
)

// AdmitRegistered queues a previously registered qualified donor graph only
// after all current draft/session/qualification/manual locks succeed. Existing
// SQL sealing owns activation and event/outbox writes in this same transaction.
// Unknown commit requires queued-receipt recovery, not blind re-registration.
func (s SQLDonorDescriptorStore) AdmitRegistered(ctx context.Context, who auth.Identity, tenant, project, shoot, run, workflow uuid.UUID,
	ordinal int, frozen FrozenDonorTask, loaded LoadedDonorTaskPlan, prepared PreparedDonorInputEvidence) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil || tenant == uuid.Nil || project == uuid.Nil || shoot == uuid.Nil || run == uuid.Nil || workflow == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || frozen.qualification == nil {
		return v2input.ErrInvalid
	}
	manual, err := CompileDonorManualAdmissionBinding(ctx, loaded, prepared)
	if err != nil {
		return err
	}
	wire, err := canonical.Marshal(donorManualAdmissionWire{manual.guardPixel, manual.original, manual.root, manual.targetPixel, manual.vectors})
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
	if err = tx.QueryRow(bounded, "SELECT photo.lock_v28_donor_graph_owner($1,$2,$3,$4)", tenant, run, who.UserID, who.SessionID).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrExpired
	}
	if err = LockQualifiedDonorDraft(bounded, tx, who, tenant, project, shoot, run, workflow, ordinal, frozen, loaded, prepared); err != nil {
		return err
	}
	if err = tx.QueryRow(bounded, "SELECT photo.seal_v28_donor_graph($1,$2,$3,$4,$5::jsonb)", tenant, run, who.UserID, who.SessionID, string(wire)).Scan(&ready); err != nil {
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
