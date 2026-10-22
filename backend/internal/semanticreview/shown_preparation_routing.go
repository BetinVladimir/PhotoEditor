package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
)

// ShownApplyResolution is produced by scoped, version-pinned physical readers.
// Resolver must repeat current review/receipt authority through Recheck.
type ShownApplyResolution struct {
	RetouchInputs           []ShownRetouchPreparationInput
	BackgroundCleanupInputs []ShownBackgroundCleanupPreparationInput
	Review                  PreparedReview
	Targets                 []LockedTarget
	Inputs                  []ShownPreparationInput
	RemovalInputs           []ShownRemovalPreparationInput
	Recheck                 func(context.Context) error
}

type ShownApplyResolver func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, ApplyDecision) (ShownApplyResolution, error)

func NewShownApplyPreparation(db *pgxpool.Pool, objects artifactjson.Objects, bucket string,
	fallback ApplyPreparation, resolve ShownApplyResolver) (ApplyPreparation, error) {
	if db == nil || objects == nil || fallback == nil || resolve == nil || len(bucket) < 3 || len(bucket) > 63 {
		return nil, ErrInvalid
	}
	return func(ctx context.Context, who auth.Identity, tenant, intent uuid.UUID, request ApplyDecision) (PreparedApply, error) {
		empty := PreparedApply{}
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		raw, err := canonical.Marshal(request)
		if err != nil {
			return empty, ErrInvalid
		}
		owned, err := DecodeApplyDecision(raw)
		if err != nil {
			return empty, err
		}
		approvals, selected := 0, 0
		for _, decision := range owned.TargetDecisions {
			if decision.Decision == "approve" {
				approvals++
				if decision.Preview != nil {
					selected++
				}
			}
		}
		if selected == 0 {
			return fallback(ctx, who, tenant, intent, owned)
		}
		if selected != approvals {
			return empty, ErrInvalid
		}
		child := uuid.New()
		resolution, err := resolve(ctx, who, tenant, intent, child, owned)
		if err != nil {
			return empty, err
		}
		// Resolver callbacks cannot change the immutable request fingerprint.
		owned, err = DecodeApplyDecision(raw)
		if err != nil {
			return empty, err
		}
		if len(resolution.RetouchInputs) > 0 {
			if len(resolution.Inputs)+len(resolution.RemovalInputs)+len(resolution.BackgroundCleanupInputs) != 0 {
				return empty, ErrInvalid
			}
			retouch, err := PrepareShownRetouchApplyBatch(ctx, db, objects, bucket, who, tenant, intent, child, resolution.Review,
				resolution.Targets, owned, resolution.RetouchInputs, resolution.Recheck)
			if err != nil {
				return empty, err
			}
			return retouch.asPreparedApply(), nil
		}
		if len(resolution.BackgroundCleanupInputs) > 0 {
			if len(resolution.Inputs) != 0 || len(resolution.RemovalInputs) != 0 {
				return empty, ErrInvalid
			}
			cleanup, err := PrepareShownBackgroundCleanupApplyBatch(ctx, db, objects, bucket, who, tenant, intent, child, resolution.Review,
				resolution.Targets, owned, resolution.BackgroundCleanupInputs, resolution.Recheck)
			if err != nil {
				return empty, err
			}
			return PreparedApply{Review: cleanup.Review, Targets: cleanup.Targets, Decision: cleanup.Decision, Child: cleanup.Child, ShownBackgroundCleanupTasks: cleanup.ShownTasks}, nil
		}
		if len(resolution.RemovalInputs) > 0 {
			if len(resolution.Inputs) != 0 || len(resolution.BackgroundCleanupInputs) != 0 {
				return empty, ErrInvalid
			}
			removal, err := PrepareShownRemovalApplyBatch(ctx, db, objects, bucket, who, tenant, intent, child, resolution.Review,
				resolution.Targets, owned, resolution.RemovalInputs, resolution.Recheck)
			if err != nil {
				return empty, err
			}
			return PreparedApply{Review: removal.Review, Targets: removal.Targets, Decision: removal.Decision, Child: removal.Child, ShownRemovalTasks: removal.ShownTasks}, nil
		}
		return PrepareShownApplyBatch(ctx, db, objects, bucket, who, tenant, intent, child, resolution.Review,
			resolution.Targets, owned, resolution.Inputs, resolution.Recheck)
	}, nil
}
