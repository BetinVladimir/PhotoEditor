package delivery

import (
	"context"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// FailPackage repeats current authority in SQL; witness is only a slot selector.
// Caller must roll back on error. Reservations remain until cleanup settles.
func (s ExpansionLeaseStore) FailPackage(ctx context.Context, lease ExpansionLease, plan PreparePlan, witness FailureWitness) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil || !lease.valid() || lease.Workflow != plan.workflow || lease.ParentTask != plan.task || witness.SlotIndex < 0 || witness.SlotIndex >= lease.Cursor || !reasonPattern.MatchString(witness.Reason) {
		return v2input.ErrInvalid
	}
	page, err := plan.ExportChildren(lease.Run, witness.SlotIndex, 1)
	if err != nil {
		return err
	}
	if len(page.Items) != 1 || page.Items[0].RunID != witness.ChildRun {
		return v2input.ErrInvalid
	}
	export, err := plan.NewChildExportID(lease.Run, page.Items[0])
	if err != nil || export != witness.ExportID {
		return v2input.ErrInvalid
	}
	var accepted bool
	if err := s.DB.QueryRow(ctx, `SELECT photo.fail_v30_delivery($1,$2,$3)`, lease.Expansion, lease.Token, witness.SlotIndex).Scan(&accepted); err != nil {
		return err
	}
	if !accepted {
		return domain.NewError("DELIVERY_FAILURE_STALE", 409)
	}
	return nil
}
