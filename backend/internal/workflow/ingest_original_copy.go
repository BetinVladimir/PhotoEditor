package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
)

type OriginalReservations interface {
	Reserve(context.Context, OriginalScope) (OriginalReservation, error)
	Complete(context.Context, OriginalScope, OriginalReservation, string) (bool, error)
}
type OriginalSourceReader interface {
	Source(context.Context, OriginalScope, OriginalReservation) (objectstore.Object, error)
}
type OriginalCopier interface {
	CopyOriginal(context.Context, uuid.UUID, uuid.UUID, objectstore.Object) (objectstore.Object, error)
}
type OriginalCoordinator struct {
	Reservations OriginalReservations
	Sources      OriginalSourceReader
	Objects      OriginalCopier
}

func (r OriginalRepository) Source(ctx context.Context, s OriginalScope, res OriginalReservation) (objectstore.Object, error) {
	if r.DB == nil || !s.valid() || res.Ready || res.ArtifactID == uuid.Nil || res.Token == uuid.Nil {
		return objectstore.Object{}, errors.New("invalid original source request")
	}
	var bucket string
	var o objectstore.Object
	err := r.DB.QueryRow(ctx, "SELECT bucket,object_key,object_version,sha256,bytes,media_type FROM photo.ingest_original_source($1,$2,$3,$4,$5)", s.Tenant, s.Run, s.Ordinal, res.ArtifactID, res.Token).Scan(&bucket, &o.Key, &o.VersionID, &o.SHA256, &o.Bytes, &o.ContentType)
	if err != nil {
		return objectstore.Object{}, err
	}
	if bucket != r.Bucket || o.SHA256 != s.Source.SHA256 || o.Key == "" || o.VersionID == "" || o.VersionID == "null" || len(o.VersionID) > 1024 || o.Bytes < 1 || o.Bytes > 536870912 || o.ContentType == "" {
		return objectstore.Object{}, errors.New("invalid original source binding")
	}
	return o, nil
}

// Prepare creates a ready original artifact, not an asset/version or collection
// item. Uncertain completion is recovered by Reserve on retry. Failed copies keep
// the durable reservation for retry/cleanup; they never discard source staging.
func (c OriginalCoordinator) Prepare(ctx context.Context, s OriginalScope) (review.ArtifactRef, error) {
	empty := review.ArtifactRef{}
	if c.Reservations == nil || c.Sources == nil || c.Objects == nil || !s.valid() {
		return empty, errors.New("invalid original coordinator")
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	res, err := c.Reservations.Reserve(ctx, s)
	if err != nil {
		return empty, err
	}
	if res.ArtifactID == uuid.Nil || res.ArtifactID == s.Source.ID || res.Token == uuid.Nil {
		return empty, errors.New("invalid original reservation")
	}
	ref := review.ArtifactRef{ID: res.ArtifactID, SHA256: s.Source.SHA256}
	if res.Ready {
		if res.Version == "" || res.Version == "null" || len(res.Version) > 1024 {
			return empty, errors.New("invalid saved original version")
		}
		return ref, nil
	}
	if res.Version != "" {
		return empty, errors.New("unexpected pending original version")
	}
	source, err := c.Sources.Source(ctx, s, res)
	if err != nil {
		return empty, err
	}
	if source.SHA256 != s.Source.SHA256 || source.Bytes < 1 || source.Bytes > 536870912 || source.VersionID == "" || source.VersionID == "null" {
		return empty, errors.New("original source mismatch")
	}
	target, err := c.Objects.CopyOriginal(ctx, s.Tenant, res.ArtifactID, source)
	if err != nil {
		return empty, err
	}
	key, err := objectstore.OriginalKey(s.Tenant, res.ArtifactID)
	if err != nil {
		return empty, err
	}
	if target.Key != key || target.SHA256 != source.SHA256 || target.Bytes != source.Bytes || target.ContentType != source.ContentType || target.VersionID == "" || target.VersionID == "null" || len(target.VersionID) > 1024 {
		return empty, errors.New("copied original mismatch")
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	complete, err := c.Reservations.Complete(ctx, s, res, target.VersionID)
	if err != nil {
		return empty, err
	}
	if !complete {
		return empty, errors.New("original completion fence lost")
	}
	return ref, nil
}
