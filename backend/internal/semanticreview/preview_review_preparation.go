package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"strconv"
)

type ScopedPreviewReview struct {
	Review   PreparedReview
	Target   LockedTarget
	Workflow uuid.UUID
}

// PrepareScopedPreviewReview resolves proposal documents without a close or Apply decision.
func PrepareScopedPreviewReview(ctx context.Context, db *pgxpool.Pool, who auth.Identity, tenant, intent, target uuid.UUID,
	request PreviewRequest, read PlanObjectReader, budget int64, modelSHA, datasetSHA string) (ScopedPreviewReview, error) {
	if db == nil || read == nil || tenant == uuid.Nil || intent == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return ScopedPreviewReview{}, ErrInvalid
	}
	if target == uuid.Nil || request.Validate() != nil {
		return ScopedPreviewReview{}, ErrInvalid
	}
	if err := CheckCurrentPreviewAuthority(ctx, db, who, tenant, intent, target, request); err != nil {
		return ScopedPreviewReview{}, err
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
		return ScopedPreviewReview{}, err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return ScopedPreviewReview{}, err
	}
	defer tx.Rollback(context.Background())
	identity, err := LoadReviewIdentity(ctx, tx, tenant, intent, who.UserID, who.SessionID)
	if err != nil {
		return ScopedPreviewReview{}, err
	}
	if identity.RunID != prepared.Sources.RunID {
		return ScopedPreviewReview{}, ErrConflict
	}
	if _, err = LoadLockedPreparedSources(ctx, tx, tenant, identity.WorkflowID, who.UserID, who.SessionID, prepared); err != nil {
		return ScopedPreviewReview{}, err
	}
	targets, err := LoadLockedTargets(ctx, tx, tenant, identity.WorkflowID, who.UserID, who.SessionID, prepared.Sources.Revision)
	if err != nil {
		return ScopedPreviewReview{}, err
	}
	var selected *LockedTarget
	for i := range targets {
		if targets[i].ID == target {
			selected = &targets[i]
			break
		}
	}
	if selected == nil || selected.State != "proposed" || selected.ChildRunID != nil || selected.BatchID != nil ||
		strconv.FormatInt(selected.Revision, 10) != request.TargetRevision ||
		strconv.FormatInt(prepared.Sources.Revision, 10) != request.ExpectedRevision || prepared.PlanRef.SHA256 != request.PlanSHA {
		return ScopedPreviewReview{}, ErrConflict
	}
	if err = tx.Commit(ctx); err != nil {
		return ScopedPreviewReview{}, err
	}
	if err := CheckCurrentPreviewAuthority(ctx, db, who, tenant, intent, target, request); err != nil {
		return ScopedPreviewReview{}, err
	}
	return ScopedPreviewReview{Review: prepared, Target: *selected, Workflow: identity.WorkflowID}, nil
}
