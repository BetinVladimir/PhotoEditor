package semanticreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type backgroundCleanupPreviewDocumentRepository struct {
	db                             *pgxpool.Pool
	scope                          artifactjson.Scope
	intent, actor, session, target uuid.UUID
	body, paramsSHA, bucket        string
	params                         v2input.Ref
}
type backgroundCleanupPreviewDocumentPublisher struct {
	repository backgroundCleanupPreviewDocumentRepository
	objects    artifactjson.Objects
	task       uuid.UUID
}

// NewBackgroundCleanupPreviewPublisher binds all document tickets to one saved preview
// and authenticated editor session. SQL repeats the live publication lock on
// both sides of immutable object IO; queue/adoption remain separate gates.
func NewBackgroundCleanupPreviewPublisher(db *pgxpool.Pool, objects artifactjson.Objects, bucket string,
	actor, session, preview uuid.UUID, frozen FrozenBackgroundCleanupPreviewTask) (v2input.Publisher, error) {
	draft, err := frozen.DraftTask()
	if err != nil {
		return nil, err
	}
	var params BackgroundCleanupPreviewParams
	if decodeStrict(frozen.frozen.closure.ParamsJCS, &params) != nil {
		return nil, ErrInvalid
	}
	body, sha, err := FreezeBackgroundCleanupPreviewParams(params)
	if err != nil || string(body) != string(frozen.frozen.closure.ParamsJCS) || sha != frozen.frozen.params.SHA256 ||
		db == nil || objects == nil || len(bucket) < 3 || len(bucket) > 63 || actor == uuid.Nil || session == uuid.Nil ||
		preview == uuid.Nil || preview == params.WorkflowID || draft.ID != uuid.NewSHA1(preview, []byte("semantic.preview_background_cleanup:"+params.TargetID.String())) {
		return nil, ErrInvalid
	}
	repo := backgroundCleanupPreviewDocumentRepository{db: db, scope: artifactjson.Scope{Tenant: params.TenantID, Run: preview, Owner: draft.ID},
		intent: params.IntentID, actor: actor, session: session, target: params.TargetID, body: string(body), paramsSHA: sha, bucket: bucket}
	return backgroundCleanupPreviewDocumentPublisher{repository: repo, objects: objects, task: draft.ID}, nil
}

func (p backgroundCleanupPreviewDocumentPublisher) Publish(ctx context.Context, task uuid.UUID, ordinal int, contract string, body []byte) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	if task != p.task {
		return v2input.Ref{}, ErrInvalid
	}
	var purpose string
	switch contract {
	case "Params":
		if ordinal != 0 || string(body) != p.repository.body {
			return v2input.Ref{}, ErrInvalid
		}
		purpose = "task_params"
	case "InputClosureChunk":
		if ordinal < 0 || ordinal >= v2input.MaxChunks {
			return v2input.Ref{}, ErrInvalid
		}
		purpose = fmt.Sprintf("v2_input_chunk_%d", ordinal)
	case "InputClosureManifest":
		if ordinal < 1 || ordinal > v2input.MaxChunks {
			return v2input.Ref{}, ErrInvalid
		}
		purpose = "v2_input_manifest"
	default:
		return v2input.Ref{}, ErrInvalid
	}
	repo := p.repository
	repo.scope.Purpose = purpose
	sum := sha256.Sum256(body)
	namespace := purpose
	if purpose == "task_params" {
		namespace = "Params"
	}
	repo.params = v2input.Ref{ArtifactID: uuid.NewSHA1(task, []byte(namespace)), SHA256: hex.EncodeToString(sum[:])}
	publisher := artifactjson.Publisher{Repository: repo, Objects: p.objects}
	ref, err := publisher.Publish(ctx, repo.scope, body)
	if err != nil {
		return v2input.Ref{}, err
	}
	if ref.ID != repo.params.ArtifactID || ref.SHA256 != repo.params.SHA256 {
		return v2input.Ref{}, ErrInvalid
	}
	return repo.params, nil
}

func (r backgroundCleanupPreviewDocumentRepository) Reserve(ctx context.Context, scope artifactjson.Scope, sha string, size int64) (artifactjson.Reservation, error) {
	if scope != r.scope || sha != r.params.SHA256 || !shaPattern.MatchString(sha) || size < 2 || size > 1<<20 {
		return artifactjson.Reservation{}, ErrInvalid
	}
	var outcome string
	var id, token *uuid.UUID
	var ready bool
	err := r.db.QueryRow(ctx, `SELECT outcome,artifact_id,token,ready FROM photo.reserve_semantic_background_cleanup_preview_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, scope.Tenant, r.intent, r.actor, r.session, r.target, scope.Run, r.body, r.paramsSHA, scope.Purpose, scope.Owner, r.params.ArtifactID, sha, size, r.bucket).Scan(&outcome, &id, &token, &ready)
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

func (r backgroundCleanupPreviewDocumentRepository) Complete(ctx context.Context, scope artifactjson.Scope, ticket artifactjson.Reservation, object objectstore.Object) (artifactjson.Ref, error) {
	key, err := artifactjson.DocumentKey(scope, ticket.ArtifactID)
	if err != nil || scope != r.scope || ticket.ArtifactID != r.params.ArtifactID || ticket.Token == uuid.Nil || object.SHA256 != r.params.SHA256 || object.Key != key || !shaPattern.MatchString(object.SHA256) || object.Bytes < 2 || object.Bytes > 1<<20 || object.ContentType != "application/json" || object.VersionID == "" || object.VersionID == "null" || len(object.VersionID) > 1024 {
		return artifactjson.Ref{}, ErrInvalid
	}
	var done bool
	err = r.db.QueryRow(ctx, `SELECT photo.complete_semantic_background_cleanup_preview_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`, scope.Tenant, r.intent, r.actor, r.session, r.target, scope.Run, r.body, r.paramsSHA, scope.Purpose, scope.Owner, ticket.ArtifactID, ticket.Token, object.SHA256, object.Bytes, r.bucket, object.VersionID).Scan(&done)
	if err != nil {
		return artifactjson.Ref{}, err
	}
	if !done {
		return artifactjson.Ref{}, artifactjson.ErrStale
	}
	return artifactjson.Ref{ID: ticket.ArtifactID, SHA256: object.SHA256}, nil
}
