package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
)

// NewRejectionPreparation installs the complete reject-only preparation path.
// Approved requests require a separately qualified execution preparer; this
// installation rejects them before any database or object IO.
func NewRejectionPreparation(db *pgxpool.Pool, objects artifactjson.Objects, read PlanObjectReader,
	bucket string, budget int64, modelSHA, datasetSHA string) (ApplyPreparation, error) {
	if db == nil || objects == nil || read == nil || len(bucket) < 3 || len(bucket) > 63 || budget < 2 || !shaPattern.MatchString(modelSHA) || !shaPattern.MatchString(datasetSHA) {
		return nil, ErrInvalid
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
		for _, decision := range owned.TargetDecisions {
			if decision.Decision != "reject" {
				return PreparedApply{}, ErrInvalid
			}
		}
		prepared, targets, err := PrepareScopedApplyReview(ctx, db, who, tenant, intent, owned, read, budget, modelSHA, datasetSHA)
		if err != nil {
			return PreparedApply{}, err
		}
		published, err := PublishDecision(ctx, db, objects, bucket, tenant, intent, who.UserID, who.SessionID, prepared, targets, owned)
		if err != nil {
			return PreparedApply{}, err
		}
		return PreparedApply{Review: prepared, Targets: targets, Decision: published}, nil
	}, nil
}

// PrepareScopedApplyReview reconstructs original review documents with reads at
// exact object versions outside transactions. Trusted qualification pins must
// be supplied by the installation, never by the HTTP request. It publishes no
// objects and admits no child; final commit must recheck all source authority.
func PrepareScopedApplyReview(ctx context.Context, db *pgxpool.Pool, who auth.Identity, tenant, intent uuid.UUID,
	request ApplyDecision, read PlanObjectReader, budget int64, modelSHA, datasetSHA string) (PreparedReview, []LockedTarget, error) {
	if db == nil || read == nil || tenant == uuid.Nil || intent == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return PreparedReview{}, nil, ErrInvalid
	}
	raw, err := canonical.Marshal(request)
	if err != nil {
		return PreparedReview{}, nil, ErrInvalid
	}
	owned, err := DecodeApplyDecision(raw)
	if err != nil {
		return PreparedReview{}, nil, err
	}
	// Every invocation resolves current authorization. No database transaction
	// spans the versioned document IO in PrepareCurrentReview.
	load := func(ctx context.Context) (PlanSources, error) {
		tx, err := db.Begin(ctx)
		if err != nil {
			return PlanSources{}, err
		}
		defer tx.Rollback(context.Background())
		identity, err := LoadReviewIdentity(ctx, tx, tenant, intent, who.UserID, who.SessionID)
		if err != nil {
			return PlanSources{}, err
		}
		sources, err := LoadPlanSources(ctx, tx, tenant, identity.WorkflowID, who.UserID, who.SessionID)
		if err != nil {
			return PlanSources{}, err
		}
		if sources.RunID != identity.RunID {
			return PlanSources{}, ErrConflict
		}
		if err = tx.Commit(ctx); err != nil {
			return PlanSources{}, err
		}
		return sources, nil
	}
	prepared, err := PrepareCurrentReview(ctx, load, read, budget, modelSHA, datasetSHA)
	if err != nil {
		return PreparedReview{}, nil, err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return PreparedReview{}, nil, err
	}
	defer tx.Rollback(context.Background())
	identity, err := LoadReviewIdentity(ctx, tx, tenant, intent, who.UserID, who.SessionID)
	if err != nil {
		return PreparedReview{}, nil, err
	}
	if identity.RunID != prepared.Sources.RunID {
		return PreparedReview{}, nil, ErrConflict
	}
	if _, err = LoadLockedPreparedSources(ctx, tx, tenant, identity.WorkflowID, who.UserID, who.SessionID, prepared); err != nil {
		return PreparedReview{}, nil, err
	}
	targets, err := LoadLockedTargets(ctx, tx, tenant, identity.WorkflowID, who.UserID, who.SessionID, prepared.Sources.Revision)
	if err != nil {
		return PreparedReview{}, nil, err
	}
	if _, err = BuildDecisionProof(prepared, targets, owned); err != nil {
		return PreparedReview{}, nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return PreparedReview{}, nil, err
	}
	return prepared, targets, nil
}
