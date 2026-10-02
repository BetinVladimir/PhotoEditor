package objectstore

import (
	"context"
	"github.com/minio/minio-go/v7"
)

// RemoveConfirmed deletes only the ticket's exact version and confirms its
// absence. A DELETE response alone must not release quota. The caller owns the
// durable DB cleanup ticket and has already fenced all new artifact references.
func (s *Store) RemoveConfirmed(ctx context.Context, object Object) error {
	if !object.valid() {
		return ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	deleteErr := s.Remove(ctx, object)
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := s.client.StatObject(ctx, s.bucket, object.Key, minio.StatObjectOptions{VersionID: object.VersionID})
	if err == nil {
		return ErrUnavailable
	}
	switch minio.ToErrorResponse(err).Code {
	case "NoSuchKey", "NoSuchVersion":
		return nil
	case "NoSuchBucket":
		return ErrUnavailable
	}
	if deleteErr != nil {
		return deleteErr
	}
	return classify(err)
}
