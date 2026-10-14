package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CreateSourceVerifiedExposureDraftInTransaction consumes the original physical
// preflight projection, rechecks source authority in the transaction, then
// composes draft/quota/input registration/seal. Inputs cannot be substituted by
// a caller. Receipt and tenant/quota locks must already be held. Caller must
// roll back on any error and finish activation, parent/receipt/events atomically.
// This does not authorize publication, dispatch or a partial transaction commit.
func CreateSourceVerifiedExposureDraftInTransaction(ctx context.Context, tx pgx.Tx,
	tenant, project, intent, actor, session, child uuid.UUID, prepared PreparedReview, targets []LockedTarget,
	request ApplyDecision, decision PublishedDecision, scope PreparedExposureSourceScope, published []PreparedExposureTask) (ExposureCommitInputs, error) {
	empty := ExposureCommitInputs{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if tx == nil || project == uuid.Nil || !scope.matches(tenant, intent, actor, session, child, prepared, request) {
		return empty, ErrInvalid
	}
	inputs, err := scope.Inputs()
	if err != nil {
		return empty, err
	}
	// Reject modified publication before acquiring source locks.
	if _, err := PrepareApprovedExposureCommitInputs(ctx, tenant, actor, child, prepared, targets, request, decision, inputs, published); err != nil {
		return empty, err
	}
	if err := scope.RecheckInTransaction(ctx, tx, child); err != nil {
		return empty, err
	}
	return createApprovedExposureDraftInTransaction(ctx, tx, tenant, project, intent, actor, session, child, prepared, targets, request, decision, inputs, published)
}
