package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"strconv"
)

func NewClosePreparation(db *pgxpool.Pool, objects artifactjson.Objects, read PlanObjectReader, bucket string, budget int64, modelSHA, datasetSHA string) (ClosePreparation, error) {
	if db == nil || objects == nil || read == nil || len(bucket) < 3 || len(bucket) > 63 || budget < 2 || budget > 64*1048576 || !shaPattern.MatchString(modelSHA) || !shaPattern.MatchString(datasetSHA) {
		return nil, ErrInvalid
	}
	return func(ctx context.Context, who auth.Identity, tenant, intent uuid.UUID, request ReviewClose) (PreparedClose, error) {
		prepared, targets, err := PrepareScopedCloseReview(ctx, db, who, tenant, intent, request, read, budget, modelSHA, datasetSHA)
		if err != nil {
			return PreparedClose{}, err
		}
		published, err := PublishClose(ctx, db, objects, bucket, tenant, intent, who.UserID, who.SessionID, prepared, targets, request)
		if err != nil {
			return PreparedClose{}, err
		}
		return PreparedClose{Review: prepared, Targets: targets, Decision: published}, nil
	}, nil
}

// PrepareScopedCloseReview verifies pinned original review documents outside
// transactions and takes a fresh source/target fence before publication.
func PrepareScopedCloseReview(ctx context.Context, db *pgxpool.Pool, who auth.Identity, tenant, intent uuid.UUID,
	request ReviewClose, read PlanObjectReader, budget int64, modelSHA, datasetSHA string) (PreparedReview, []LockedTarget, error) {
	if err := ctx.Err(); err != nil {
		return PreparedReview{}, nil, err
	}
	if db == nil || read == nil || tenant == uuid.Nil || intent == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return PreparedReview{}, nil, ErrInvalid
	}
	raw, err := canonical.Marshal(request)
	if err != nil {
		return PreparedReview{}, nil, ErrInvalid
	}
	owned, err := DecodeReviewClose(raw)
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
	if _, err = BuildCloseProof(prepared.Sources.RunID, owned, prepared.PlanRef.SHA256, strconv.FormatInt(prepared.Sources.Revision, 10), "waiting_review", targets); err != nil {
		return PreparedReview{}, nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return PreparedReview{}, nil, err
	}
	return prepared, targets, nil
}
