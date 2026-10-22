package v2optical

import (
	"bytes"
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// LockNegativeAdmission rebuilds the exact qualified inventory before SQL, then
// retains source membership, every array occurrence and located qualification
// locks through the caller's transaction. It does not read objects, register a
// graph, commit or assert that physical array verification has taken place.
func LockNegativeAdmission(ctx context.Context, tx pgx.Tx, who auth.Identity,
	tenant, project, snapshot uuid.UUID, plan NegativeTaskPlan, source []byte,
	located LocatedNegativeQualification, published v2input.Ref) ([]v2input.Entry, []v2input.Ref, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if tx == nil || tenant == uuid.Nil || project == uuid.Nil || snapshot == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return nil, nil, v2input.ErrInvalid
	}
	// Own the bytes used again after the source lock may have waited.
	plan.Params = bytes.Clone(plan.Params)
	plan.Profile = bytes.Clone(plan.Profile)
	source = bytes.Clone(source)
	qualified, err := located.Qualification()
	if err != nil {
		return nil, nil, err
	}
	entries, direct, err := NegativeTaskInputEntries(ctx, plan, source, qualified, published)
	if err != nil {
		return nil, nil, err
	}
	if err = LockNegativeSourceInputs(ctx, tx, who, tenant, project, snapshot, plan.Params, source, qualified.pin.Report); err != nil {
		return nil, nil, err
	}
	if err = LockLocatedNegativeQualification(ctx, tx, who, tenant, project, located); err != nil {
		return nil, nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, nil, err
	}
	return entries, direct, nil
}
