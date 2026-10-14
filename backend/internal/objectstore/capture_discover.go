package objectstore

import (
	"context"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
)

// DiscoverCaptureMultipart observes one exact staging key. An empty result is
// never evidence that an in-flight CreateMultipartUpload has terminated.
func (s *Store) DiscoverCaptureMultipart(ctx context.Context, tenant, upload uuid.UUID) (string, error) {
	key, err := StagingKey(tenant, upload)
	if err != nil || s == nil || s.client == nil || s.bucket == "" {
		return "", ErrInvalid
	}
	return discoverExactMultipart(ctx, key, func(scan context.Context) <-chan minio.ObjectMultipartInfo {
		return s.client.ListIncompleteUploads(scan, s.bucket, key, true)
	})
}
