package delivery

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// FailSignedPackage delegates the final transition to SQL, which re-reads the
// deterministic signer status, package item and current complete parent lease.
func (s ExpansionLeaseStore) FailSignedPackage(ctx context.Context,
	lease ExpansionLease, plan PreparePlan, slot Slot) error {
	if err := ctx.Err(); err != nil { return err }
	if s.DB == nil || !lease.valid() || lease.ParentTask != plan.TaskID() ||
		lease.Cursor != lease.SlotCount || slot.Ordinal < 0 ||
		slot.PresetID == uuid.Nil || slot.State != "ready" {
		return v2input.ErrInvalid
	}
	var accepted bool
	if err := s.DB.QueryRow(ctx, `SELECT photo.fail_v31_delivery_signing($1,$2,$3,$4)`,
		lease.Expansion, lease.Token, slot.Ordinal, slot.PresetID).Scan(&accepted); err != nil {
		return err
	}
	if !accepted { return domain.NewError("DELIVERY_SIGNING_FAILURE_STALE", 409) }
	return ctx.Err()
}
