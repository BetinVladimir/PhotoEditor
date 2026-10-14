package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
)

type ExposureApplyObjects interface {
	artifactjson.Objects
	planObjectStore
}

// ExposureBudgetSelection selects trusted deployment qualification and measured
// owner costs. It cannot confer source authority or bypass final SQL checks.
type ExposureBudgetSelection func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, ApplyDecision) ([]ExposureInputBudget, error)

// NewExposurePreparation installs rejection and local-exposure approval paths
// for ApplyService. It does not enable other semantic operation kinds.
func NewExposurePreparation(db *pgxpool.Pool, objects ExposureApplyObjects, read PlanObjectReader,
	bucket string, budget int64, modelSHA, datasetSHA string, color ExposureColorProfile, selectBudgets ExposureBudgetSelection) (ApplyPreparation, error) {
	if objects == nil || !color.valid() || selectBudgets == nil {
		return nil, ErrInvalid
	}
	reject, err := NewRejectionPreparation(db, objects, read, bucket, budget, modelSHA, datasetSHA)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, who auth.Identity, tenant, intent uuid.UUID, request ApplyDecision) (PreparedApply, error) {
		raw, err := canonical.Marshal(request)
		if err != nil {
			return PreparedApply{}, ErrInvalid
		}
		owned, err := DecodeApplyDecision(raw)
		if err != nil {
			return PreparedApply{}, err
		}
		approved := false
		for _, decision := range owned.TargetDecisions {
			approved = approved || decision.Decision == "approve"
		}
		if !approved {
			return reject(ctx, who, tenant, intent, owned)
		}
		selectionRequest, err := DecodeApplyDecision(raw)
		if err != nil {
			return PreparedApply{}, err
		}
		budgets, err := selectBudgets(ctx, who, tenant, intent, selectionRequest)
		if err != nil {
			return PreparedApply{}, err
		}
		return PrepareExposureApply(ctx, db, objects, bucket, who, tenant, intent, uuid.New(), owned, read, budget, modelSHA, datasetSHA, budgets, color)
	}, nil
}

// PrepareExposureApply publishes a complete non-dispatchable approved exposure
// bundle. Review authority is reconstructed from scoped original documents;
// budgets and qualification pins must come from a trusted installation. SQL
// commit independently checks current policy and the retained source proof.
// Publication failures can leave immutable draft objects, but no child or quota.
func PrepareExposureApply(ctx context.Context, db *pgxpool.Pool, objects ExposureApplyObjects, bucket string,
	who auth.Identity, tenant, intent, child uuid.UUID, request ApplyDecision, read PlanObjectReader,
	budget int64, modelSHA, datasetSHA string, inputs []ExposureInputBudget, color ExposureColorProfile) (PreparedApply, error) {
	empty := PreparedApply{}
	if db == nil || objects == nil || child == uuid.Nil || !color.valid() {
		return empty, ErrInvalid
	}
	raw, err := canonical.Marshal(request)
	if err != nil {
		return empty, ErrInvalid
	}
	owned, err := DecodeApplyDecision(raw)
	if err != nil {
		return empty, err
	}
	// Copy value-only deployment budgets before callbacks and object IO.
	budgets := append([]ExposureInputBudget(nil), inputs...)
	review, targets, err := PrepareScopedApplyReview(ctx, db, who, tenant, intent, owned, read, budget, modelSHA, datasetSHA)
	if err != nil {
		return empty, err
	}
	verifiedPins := make(map[ExposureQualificationPin]bool)
	for _, input := range budgets {
		if verifiedPins[input.Qualification] {
			continue
		}
		if err = VerifyExposureReviewReport(ctx, db, read, bucket, tenant, intent, who.UserID, who.SessionID, review.Sources.Revision, input.Qualification); err != nil {
			return empty, err
		}
		verifiedPins[input.Qualification] = true
	}
	scope, err := PrepareApprovedExposureSourceScope(ctx, db, objects, bucket, tenant, intent, who.UserID, who.SessionID, child, review, targets, owned, budgets, color)
	if err != nil {
		return empty, err
	}
	resolved, err := scope.Inputs()
	if err != nil {
		return empty, err
	}
	frozen, err := CompileApprovedExposureTasks(child, review, targets, owned, resolved)
	if err != nil {
		return empty, err
	}
	byTarget := make(map[uuid.UUID]LockedTarget, len(targets))
	for _, target := range targets {
		byTarget[target.ID] = target
	}
	// Params have their own authenticated pre-child registration, distinct from
	// transport chunks. The draft shell requires both inventories to be ready.
	if _, err = PublishApprovedTaskParams(ctx, db, objects, bucket, tenant, intent, who.UserID, who.SessionID, child, review, targets, owned); err != nil {
		return empty, err
	}
	result := PreparedApply{Review: review, Targets: targets, Child: child, SourceScope: scope, Tasks: make([]PreparedExposureTask, 0, len(frozen))}
	for _, task := range frozen {
		target, ok := byTarget[task.target]
		if !ok {
			return empty, ErrInvalid
		}
		publisher, err := NewApprovedExposurePublisher(db, objects, bucket, tenant, intent, who.UserID, who.SessionID, review.Sources.RunID, child, review.Sources.Revision, target, task)
		if err != nil {
			return empty, err
		}
		published, err := PublishExposureTask(ctx, task, publisher)
		if err != nil {
			return empty, err
		}
		result.Tasks = append(result.Tasks, published)
	}
	result.Decision, err = PublishDecision(ctx, db, objects, bucket, tenant, intent, who.UserID, who.SessionID, review, targets, owned)
	if err != nil {
		return empty, err
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	return result, nil
}
