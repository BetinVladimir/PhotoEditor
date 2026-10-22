package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/objectstore"
)

type planObjectStore interface {
	Open(context.Context, objectstore.Object) (io.ReadCloser, error)
}

// NewPlanObjectReader binds the configured bucket rather than trusting a caller
// to choose storage. Store.Open validates and authenticates the exact version.
func NewPlanObjectReader(store planObjectStore, bucket string) (PlanObjectReader, error) {
	if store == nil || bucket == "" {
		return nil, ErrInvalid
	}
	return func(ctx context.Context, source PlanSource) ([]byte, error) {
		if source.Ref.ArtifactID == uuid.Nil || source.Bucket != bucket || source.MediaType != "application/json" || source.Bytes < 2 || source.Bytes > 8<<20 || !shaPattern.MatchString(source.Ref.SHA256) || source.Version == "" || source.Version == "null" {
			return nil, ErrInvalid
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		reader, err := store.Open(ctx, objectstore.Object{Key: source.Key, VersionID: source.Version, SHA256: source.Ref.SHA256, ContentType: source.MediaType, Bytes: source.Bytes})
		if err != nil {
			return nil, err
		}
        if reader == nil {
            return nil, ErrInvalid
        }
		body, readErr := io.ReadAll(io.LimitReader(reader, source.Bytes+1))
		closeErr := reader.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if int64(len(body)) != source.Bytes {
			return nil, ErrInvalid
		}
		return body, nil
	}, nil
}
