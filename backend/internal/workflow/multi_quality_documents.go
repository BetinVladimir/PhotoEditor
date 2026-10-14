package workflow

import (
	"bytes"
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

type MultiQualityDocuments struct {
	db                                        *pgxpool.Pool
	scope                                     artifactjson.Scope
	bucket, variant, sha, root, image, params string
}

var _ artifactjson.Repository = (*MultiQualityDocuments)(nil)

func NewMultiQualityDocuments(db *pgxpool.Pool, bucket string, plan MultiQualityPlan, content v2render.MultiResultContent) (*MultiQualityDocuments, error) {
	if err := validateMultiQualityIntent(plan); err != nil {
		return nil, err
	}
	if db == nil || len(bucket) < 3 || len(bucket) > 63 {
		return nil, objectstore.ErrInvalid
	}
	rebuilt, err := CompileMultiQualityPlan(plan.source, content)
	if err != nil {
		return nil, err
	}
	if rebuilt.run != plan.run || !bytes.Equal(rebuilt.params, plan.params) || !bytes.Equal(rebuilt.profile, plan.profile) {
		return nil, objectstore.ErrIntegrity
	}
	if len(rebuilt.inputs) != len(plan.inputs) {
		return nil, objectstore.ErrIntegrity
	}
	for i, ref := range rebuilt.inputs {
		if ref != plan.inputs[i] {
			return nil, objectstore.ErrIntegrity
		}
	}
	root, image, err := content.PreviewDocuments(plan.source)
	if err != nil {
		return nil, err
	}
	if len(root) < 2 || len(root) > 1048576 || len(image) < 2 || len(image) > 1048576 || len(plan.params) < 2 || len(plan.params) > 65536 {
		return nil, objectstore.ErrInvalid
	}
	return &MultiQualityDocuments{db: db, scope: artifactjson.Scope{Tenant: plan.source.Tenant, Run: plan.run, Owner: plan.task, Purpose: "task_params"}, bucket: bucket, variant: "master", sha: renderDerivativeBytesSHA(plan.params), root: string(root), image: string(image), params: string(plan.params)}, nil
}
func (r *MultiQualityDocuments) Reserve(ctx context.Context, s artifactjson.Scope, sha string, size int64) (artifactjson.Reservation, error) {
	if r == nil || r.db == nil || s != r.scope || sha != r.sha || size != int64(len(r.params)) {
		return artifactjson.Reservation{}, objectstore.ErrInvalid
	}
	var id, token uuid.UUID
	var ready bool
	err := withMultiPreviewDocumentTx(ctx, r.db, s.Tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT * FROM photo.reserve_v29_multi_quality_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)", s.Tenant, s.Run, s.Owner, r.variant, sha, size, r.bucket, r.root, r.image, r.params).Scan(&id, &token, &ready)
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
func (r *MultiQualityDocuments) Complete(ctx context.Context, s artifactjson.Scope, v artifactjson.Reservation, o objectstore.Object) (artifactjson.Ref, error) {
	if r == nil || r.db == nil || s != r.scope || v.ArtifactID == uuid.Nil || v.Token == uuid.Nil || o.SHA256 != r.sha || o.Bytes != int64(len(r.params)) || o.ContentType != "application/json" || o.VersionID == "" || o.VersionID == "null" {
		return artifactjson.Ref{}, objectstore.ErrIntegrity
	}
	key, err := artifactjson.DocumentKey(s, v.ArtifactID)
	if err != nil || o.Key != key {
		return artifactjson.Ref{}, objectstore.ErrIntegrity
	}
	var completed bool
	err = withMultiPreviewDocumentTx(ctx, r.db, s.Tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT photo.complete_v29_multi_quality_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)", s.Tenant, s.Run, s.Owner, v.ArtifactID, v.Token, o.SHA256, o.Bytes, r.bucket, o.VersionID, r.root, r.image).Scan(&completed)
	})
	if err != nil {
		return artifactjson.Ref{}, err
	}
	if !completed {
		return artifactjson.Ref{}, artifactjson.ErrStale
	}
	return artifactjson.Ref{ID: v.ArtifactID, SHA256: o.SHA256}, nil
}

func PublishMultiQualityTask(ctx context.Context, db *pgxpool.Pool, objects artifactjson.Objects, bucket string, plan MultiQualityPlan, content v2render.MultiResultContent) (PreparedTask, error) {
	if err := ctx.Err(); err != nil {
		return PreparedTask{}, err
	}
	if objects == nil {
		return PreparedTask{}, objectstore.ErrInvalid
	}
	repository, err := NewMultiQualityDocuments(db, bucket, plan, content)
	if err != nil {
		return PreparedTask{}, err
	}
	ref, err := (artifactjson.Publisher{Repository: repository, Objects: objects}).Publish(ctx, repository.scope, []byte(repository.params))
	if err != nil {
		return PreparedTask{}, err
	}
	return PrepareMultiQualityTask(plan, review.ArtifactRef{ID: ref.ID, SHA256: ref.SHA256})
}
