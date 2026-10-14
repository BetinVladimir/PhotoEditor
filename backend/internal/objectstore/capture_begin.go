package objectstore

import (
	"context"

	"github.com/google/uuid"
)

// BeginCaptureMultipart accepts only the canonical staging key and limits an
// SDK invocation to one non-idempotent CreateMultipartUpload HTTP dispatch.
// A missing response remains ambiguous even if the SDK returns an error.
func (s *Store) BeginCaptureMultipart(ctx context.Context, tenant, upload uuid.UUID, mediaType string) (string, error) {
	key, err := StagingKey(tenant, upload)
	if err != nil {
		return "", err
	}
	return s.BeginMultipart(singleInterchangeCreateContext(ctx), key, mediaType)
}
