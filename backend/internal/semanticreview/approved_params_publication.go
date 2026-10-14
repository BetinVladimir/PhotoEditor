package semanticreview

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type approvedParamsRepository struct {
	db                     *pgxpool.Pool
	scope                  artifactjson.Scope
	intent, actor, session uuid.UUID
	revision               int64
	target                 LockedTarget
	edit                   ApprovedEdit
	params                 v2input.Ref
	bucket                 string
}

// PublishApprovedTaskParams publishes verified canonical Params outside SQL
// transactions. On partial failure earlier ready documents remain retryable;
// no child or batch is created here. Final admission must recheck all pins.
func PublishApprovedTaskParams(ctx context.Context, db *pgxpool.Pool, objects artifactjson.Objects, bucket string, tenant, intent, actor, session, child uuid.UUID, prepared PreparedReview, targets []LockedTarget, request ApplyDecision) ([]ApprovedTaskDraft, error) {
	if db == nil || objects == nil || len(bucket) < 3 || len(bucket) > 63 {
		return nil, ErrInvalid
	}
	drafts, err := CompileApprovedTaskDrafts(child, prepared, targets, request)
	if err != nil {
		return nil, err
	}
	byID := make(map[uuid.UUID]LockedTarget, len(targets))
	for _, target := range targets {
		byID[target.ID] = target
	}
	runID, revision := prepared.Sources.RunID, prepared.Sources.Revision
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	identity, err := LoadReviewIdentity(ctx, tx, tenant, intent, actor, session)
	if err != nil {
		return nil, err
	}
	if identity.RunID != runID {
		return nil, ErrConflict
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	for _, draft := range drafts {
		var edit ApprovedEdit
		if json.Unmarshal(draft.Body, &edit) != nil {
			return nil, ErrInvalid
		}
		scope := artifactjson.Scope{Tenant: tenant, Run: identity.RunID, Owner: draft.TaskID, Purpose: "task_params"}
		repo := approvedParamsRepository{db: db, scope: scope, intent: intent, actor: actor, session: session, revision: revision, target: byID[draft.TargetID], edit: edit, params: draft.Params, bucket: bucket}
		publisher := artifactjson.Publisher{Repository: repo, Objects: objects}
		ref, err := publisher.Publish(ctx, scope, draft.Body)
		if err != nil {
			return nil, err
		}
		if ref.ID != draft.Params.ArtifactID || ref.SHA256 != draft.Params.SHA256 {
			return nil, ErrInvalid
		}
	}
	return drafts, nil
}

func (r approvedParamsRepository) Reserve(ctx context.Context, scope artifactjson.Scope, sha string, size int64) (artifactjson.Reservation, error) {
	if scope != r.scope || sha != r.params.SHA256 || !shaPattern.MatchString(sha) || size < 2 || size > 1<<20 {
		return artifactjson.Reservation{}, ErrInvalid
	}
	var outcome string
	var id, token *uuid.UUID
	var ready bool
	err := r.db.QueryRow(ctx, `SELECT outcome,artifact_id,token,ready FROM photo.reserve_semantic_approved_params_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, scope.Tenant, r.intent, r.actor, r.session, r.revision, r.target.ID, r.target.Revision, r.edit.Source.AssetVersionID, r.edit.Candidate.ArtifactID, r.edit.Candidate.SHA256, scope.Owner, r.params.ArtifactID, sha, size, r.bucket).Scan(&outcome, &id, &token, &ready)
	if err != nil {
		return artifactjson.Reservation{}, err
	}
	switch outcome {
	case "conflict":
		return artifactjson.Reservation{}, artifactjson.ErrConflict
	case "stale", "forbidden":
		return artifactjson.Reservation{}, artifactjson.ErrStale
	case "quota":
		return artifactjson.Reservation{}, artifactjson.ErrQuota
	case "created", "resumed", "replayed":
	default:
		return artifactjson.Reservation{}, ErrInvalid
	}
	if id == nil || *id != r.params.ArtifactID || token == nil || *token == uuid.Nil || ready != (outcome == "replayed") {
		return artifactjson.Reservation{}, ErrInvalid
	}
	result := artifactjson.Reservation{ArtifactID: *id, Token: *token}
	if ready {
		result.Ready = &artifactjson.Ref{ID: *id, SHA256: sha}
	}
	return result, nil
}

func (r approvedParamsRepository) Complete(ctx context.Context, scope artifactjson.Scope, ticket artifactjson.Reservation, object objectstore.Object) (artifactjson.Ref, error) {
	key, err := artifactjson.DocumentKey(scope, ticket.ArtifactID)
	if err != nil || scope != r.scope || ticket.ArtifactID != r.params.ArtifactID || ticket.Token == uuid.Nil || object.SHA256 != r.params.SHA256 || object.Key != key || !shaPattern.MatchString(object.SHA256) || object.Bytes < 2 || object.Bytes > 1<<20 || object.ContentType != "application/json" || object.VersionID == "" || object.VersionID == "null" || len(object.VersionID) > 1024 {
		return artifactjson.Ref{}, ErrInvalid
	}
	var done bool
	err = r.db.QueryRow(ctx, `SELECT photo.complete_semantic_approved_params_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`, scope.Tenant, r.intent, r.actor, r.session, r.revision, r.target.ID, r.target.Revision, r.edit.Source.AssetVersionID, r.edit.Candidate.ArtifactID, r.edit.Candidate.SHA256, scope.Owner, ticket.ArtifactID, ticket.Token, object.SHA256, object.Bytes, r.bucket, object.VersionID).Scan(&done)
	if err != nil {
		return artifactjson.Ref{}, err
	}
	if !done {
		return artifactjson.Ref{}, artifactjson.ErrStale
	}
	return artifactjson.Ref{ID: ticket.ArtifactID, SHA256: object.SHA256}, nil
}
