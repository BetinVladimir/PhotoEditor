package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type PerspectiveLinesTransportRepository struct {
	db                *pgxpool.Pool
	scope             artifactjson.Scope
	bucket, sha, body string
}

var _ artifactjson.Repository = (*PerspectiveLinesTransportRepository)(nil)

func newPerspectiveLinesTransportRepository(db *pgxpool.Pool, bucket string, doc PerspectiveLinesTransportDocument) (*PerspectiveLinesTransportRepository, error) {
	if db == nil || len(bucket) < 3 || len(bucket) > 63 {
		return nil, objectstore.ErrInvalid
	}
	if _, err := doc.Proof(); err != nil {
		return nil, err
	}
	if doc.scope.Purpose != "v2_input_chunk_0" && doc.scope.Purpose != "v2_input_manifest" {
		return nil, v2input.ErrInvalid
	}
	return &PerspectiveLinesTransportRepository{db: db, scope: doc.Scope(), bucket: bucket, sha: doc.SHA256(), body: string(doc.Bytes())}, nil
}
func (r *PerspectiveLinesTransportRepository) Reserve(ctx context.Context, s artifactjson.Scope, sha string, size int64) (artifactjson.Reservation, error) {
	if r == nil || r.db == nil || s != r.scope || sha != r.sha || size != int64(len(r.body)) {
		return artifactjson.Reservation{}, objectstore.ErrInvalid
	}
	var id, token uuid.UUID
	var ready bool
	err := withPerspectivePreviewDocumentTx(ctx, r.db, s.Tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT * FROM photo.reserve_v29_perspective_lines_transport($1,$2,$3,$4,$5,$6,$7,$8)", s.Tenant, s.Run, s.Owner, s.Purpose, sha, size, r.bucket, r.body).Scan(&id, &token, &ready)
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
func (r *PerspectiveLinesTransportRepository) Complete(ctx context.Context, s artifactjson.Scope, v artifactjson.Reservation, o objectstore.Object) (artifactjson.Ref, error) {
	if r == nil || r.db == nil || s != r.scope || v.ArtifactID == uuid.Nil || v.Token == uuid.Nil || o.SHA256 != r.sha || o.Bytes != int64(len(r.body)) || o.ContentType != "application/json" || o.VersionID == "" || o.VersionID == "null" || len(o.VersionID) > 1024 {
		return artifactjson.Ref{}, objectstore.ErrIntegrity
	}
	key, err := artifactjson.DocumentKey(s, v.ArtifactID)
	if err != nil || o.Key != key {
		return artifactjson.Ref{}, objectstore.ErrIntegrity
	}
	var completed bool
	err = withPerspectivePreviewDocumentTx(ctx, r.db, s.Tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT photo.complete_v29_perspective_lines_transport($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)", s.Tenant, s.Run, s.Owner, s.Purpose, v.ArtifactID, v.Token, o.SHA256, o.Bytes, r.bucket, o.VersionID, r.body).Scan(&completed)
	})
	if err != nil {
		return artifactjson.Ref{}, err
	}
	if !completed {
		return artifactjson.Ref{}, artifactjson.ErrStale
	}
	return artifactjson.Ref{ID: v.ArtifactID, SHA256: o.SHA256}, nil
}
