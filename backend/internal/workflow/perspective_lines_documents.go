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
)

type PerspectiveLinesDocuments struct {
	db                  *pgxpool.Pool
	scope               artifactjson.Scope
	bucket, sha, params string
}

var _ artifactjson.Repository = (*PerspectiveLinesDocuments)(nil)

func NewPerspectiveLinesDocuments(db *pgxpool.Pool, bucket string, plan PerspectiveLinesPlan) (*PerspectiveLinesDocuments, error) {
	params := review.ArtifactRef{ID: uuid.NewSHA1(plan.task, []byte("document-params-validation")), SHA256: renderDerivativeBytesSHA(plan.params)}
	if _, err := PreparePerspectiveLinesTask(plan, params); err != nil {
		return nil, err
	}
	if db == nil || len(bucket) < 3 || len(bucket) > 63 || len(plan.params) < 2 || len(plan.params) > 65536 {
		return nil, objectstore.ErrInvalid
	}
	return &PerspectiveLinesDocuments{db: db, scope: artifactjson.Scope{Tenant: plan.scope.Tenant, Run: plan.scope.Run, Owner: plan.task, Purpose: "task_params"}, bucket: bucket, sha: renderDerivativeBytesSHA(plan.params), params: string(plan.params)}, nil
}
func (r *PerspectiveLinesDocuments) Reserve(ctx context.Context, s artifactjson.Scope, sha string, size int64) (artifactjson.Reservation, error) {
	if r == nil || r.db == nil || s != r.scope || sha != r.sha || size != int64(len(r.params)) {
		return artifactjson.Reservation{}, objectstore.ErrInvalid
	}
	var id, token uuid.UUID
	var ready bool
	err := withPerspectivePreviewDocumentTx(ctx, r.db, s.Tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT * FROM photo.reserve_v29_perspective_line_detection_document($1,$2,$3,$4,$5,$6,$7)", s.Tenant, s.Run, s.Owner, sha, size, r.bucket, r.params).Scan(&id, &token, &ready)
	})
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
func (r *PerspectiveLinesDocuments) Complete(ctx context.Context, s artifactjson.Scope, v artifactjson.Reservation, o objectstore.Object) (artifactjson.Ref, error) {
	if r == nil || r.db == nil || s != r.scope || v.ArtifactID == uuid.Nil || v.Token == uuid.Nil || o.SHA256 != r.sha || o.Bytes != int64(len(r.params)) || o.ContentType != "application/json" || o.VersionID == "" || o.VersionID == "null" || len(o.VersionID) > 1024 {
		return artifactjson.Ref{}, objectstore.ErrIntegrity
	}
	key, err := artifactjson.DocumentKey(s, v.ArtifactID)
	if err != nil || o.Key != key {
		return artifactjson.Ref{}, objectstore.ErrIntegrity
	}
	var completed bool
	err = withPerspectivePreviewDocumentTx(ctx, r.db, s.Tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT photo.complete_v29_perspective_line_detection_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)", s.Tenant, s.Run, s.Owner, v.ArtifactID, v.Token, o.SHA256, o.Bytes, r.bucket, o.VersionID, r.params).Scan(&completed)
	})
	if err != nil {
		return artifactjson.Ref{}, err
	}
	if !completed {
		return artifactjson.Ref{}, artifactjson.ErrStale
	}
	return artifactjson.Ref{ID: v.ArtifactID, SHA256: o.SHA256}, nil
}

func PublishPerspectiveLinesTask(ctx context.Context, db *pgxpool.Pool, objects artifactjson.Objects, bucket string, plan PerspectiveLinesPlan) (PreparedTask, error) {
	if err := ctx.Err(); err != nil {
		return PreparedTask{}, err
	}
	if objects == nil {
		return PreparedTask{}, objectstore.ErrInvalid
	}
	repository, err := NewPerspectiveLinesDocuments(db, bucket, plan)
	if err != nil {
		return PreparedTask{}, err
	}
	ref, err := (artifactjson.Publisher{Repository: repository, Objects: objects}).Publish(ctx, repository.scope, []byte(repository.params))
	if err != nil {
		return PreparedTask{}, err
	}
	return PreparePerspectiveLinesTask(plan, review.ArtifactRef{ID: ref.ID, SHA256: ref.SHA256})
}
