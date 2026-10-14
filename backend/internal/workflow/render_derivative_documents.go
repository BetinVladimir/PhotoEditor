package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2render"
)

// Pool-backed calls complete before Publisher object IO; frozen strings own authored evidence.
type RenderDerivativeDocuments struct {
	db                                        *pgxpool.Pool
	scope                                     artifactjson.Scope
	bucket, variant, sha, root, image, params string
}

var _ artifactjson.Repository = (*RenderDerivativeDocuments)(nil)

// PublishRenderDerivativeTasks publishes both fixed parameter documents through
// durable SQL tickets and immutable object IO, then compiles their task bindings.
// An interrupted call can be repeated with the same intent and evidence: already
// ready documents are reused. The caller must reserve the intent's run quota
// first, and materialize/seal the returned tasks using scoped SQL afterwards.
func PublishRenderDerivativeTasks(ctx context.Context, db *pgxpool.Pool, objects artifactjson.Objects, bucket string, intent v2render.RenderDerivativeIntent, root, image []byte) ([]PreparedTask, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if db == nil || objects == nil || intent.Tenant == uuid.Nil {
		return nil, objectstore.ErrInvalid
	}
	documents, err := BuildRenderDerivativeDocuments(intent, root, image)
	if err != nil {
		return nil, err
	}
	// Own all authored evidence before any database or object-store wait.
	repositories := make([]*RenderDerivativeDocuments, len(documents))
	for i, document := range documents {
		repositories[i], err = NewRenderDerivativeDocuments(db, intent.Tenant, bucket, document, root, image)
		if err != nil {
			return nil, err
		}
	}
	tasks := make([]PreparedTask, 0, len(documents))
	for i, document := range documents {
		repository := repositories[i]
		publisher := artifactjson.Publisher{Repository: repository, Objects: objects}
		ref, err := publisher.Publish(ctx, repository.scope, []byte(repository.params))
		if err != nil {
			return nil, err
		}
		task, err := PrepareRenderDerivativeTask(document, review.ArtifactRef{ID: ref.ID, SHA256: ref.SHA256})
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

func NewRenderDerivativeDocuments(db *pgxpool.Pool, tenant uuid.UUID, bucket string, document RenderDerivativeDocument, root, image []byte) (*RenderDerivativeDocuments, error) {
	if db == nil || tenant == uuid.Nil || document.Run == uuid.Nil || document.ID != uuid.NewSHA1(document.Run, []byte("render.derivative:"+document.Variant)) || len(bucket) < 3 || len(bucket) > 63 || len(document.Params) < 2 || len(document.Params) > 1048576 || renderDerivativeBytesSHA(document.Params) != document.ParamsSHA || len(root) < 2 || len(root) > 1048576 || len(image) < 2 || len(image) > 1048576 || (document.Variant != "preview1600" && document.Variant != "preview2560") {
		return nil, objectstore.ErrInvalid
	}
	return &RenderDerivativeDocuments{db: db, scope: artifactjson.Scope{Tenant: tenant, Run: document.Run, Owner: document.ID, Purpose: "task_params"}, bucket: bucket, variant: document.Variant, sha: document.ParamsSHA, root: string(root), image: string(image), params: string(document.Params)}, nil
}
func (r *RenderDerivativeDocuments) Reserve(ctx context.Context, s artifactjson.Scope, sha string, size int64) (artifactjson.Reservation, error) {
	if r == nil || r.db == nil || s != r.scope || sha != r.sha || size != int64(len(r.params)) {
		return artifactjson.Reservation{}, objectstore.ErrInvalid
	}
	var id, token uuid.UUID
	var ready bool
	err := r.db.QueryRow(ctx, "SELECT * FROM photo.reserve_v26_render_derivative_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)", s.Tenant, s.Run, s.Owner, r.variant, sha, size, r.bucket, r.root, r.image, r.params).Scan(&id, &token, &ready)
	if errors.Is(err, pgx.ErrNoRows) {
		return artifactjson.Reservation{}, artifactjson.ErrStale
	}
	if err != nil {
		return artifactjson.Reservation{}, err
	}
	if id == uuid.Nil || token == uuid.Nil {
		return artifactjson.Reservation{}, objectstore.ErrIntegrity
	}
	reservation := artifactjson.Reservation{ArtifactID: id, Token: token}
	if ready {
		reservation.Ready = &artifactjson.Ref{ID: id, SHA256: sha}
	}
	return reservation, nil
}
func (r *RenderDerivativeDocuments) Complete(ctx context.Context, s artifactjson.Scope, v artifactjson.Reservation, o objectstore.Object) (artifactjson.Ref, error) {
	if r == nil || r.db == nil || s != r.scope || v.ArtifactID == uuid.Nil || v.Token == uuid.Nil || o.SHA256 != r.sha || o.Bytes != int64(len(r.params)) || o.ContentType != "application/json" || o.VersionID == "" || o.VersionID == "null" {
		return artifactjson.Ref{}, objectstore.ErrIntegrity
	}
	key, err := artifactjson.DocumentKey(s, v.ArtifactID)
	if err != nil || o.Key != key {
		return artifactjson.Ref{}, objectstore.ErrIntegrity
	}
	var completed bool
	err = r.db.QueryRow(ctx, "SELECT photo.complete_v26_render_derivative_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)", s.Tenant, s.Run, s.Owner, v.ArtifactID, v.Token, o.SHA256, o.Bytes, r.bucket, o.VersionID, r.root, r.image).Scan(&completed)
	if err != nil {
		return artifactjson.Ref{}, err
	}
	if !completed {
		return artifactjson.Ref{}, artifactjson.ErrStale
	}
	return artifactjson.Ref{ID: v.ArtifactID, SHA256: o.SHA256}, nil
}
