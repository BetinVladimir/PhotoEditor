package objectstore

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"io"

	"github.com/minio/minio-go/v7"
)

// RecoverImmutable resolves an uncertain PUT of a uniquely reserved key. It
// pins the discovered version and verifies actual bytes, not just metadata.
// It never overwrites or deletes an object. Only the owner of the durable key
// reservation may use the returned version to complete artifact registration.
func (s *Store) RecoverImmutable(ctx context.Context, key string, size int64, sha, contentType string) (Object, error) {
	if !validKey(key) || size < 0 || size > 1<<50 || !hashPattern.MatchString(sha) || !mimePattern.MatchString(contentType) {
		return Object{}, ErrInvalid
	}
	stat, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return Object{}, classify(err)
	}
	object := Object{Key: key, VersionID: stat.VersionID, SHA256: sha, ContentType: contentType, Bytes: size}
	if !object.valid() || stat.Size != size || stat.ContentType != contentType || stat.Metadata.Get("X-Amz-Meta-Sha256") != sha {
		return Object{}, ErrIntegrity
	}
	reader, err := s.Open(ctx, object)
	if err != nil {
		return Object{}, err
	}
	_, copyErr := io.Copy(io.Discard, reader)
	closeErr := reader.Close()
	if copyErr != nil {
		return Object{}, copyErr
	}
	if closeErr != nil {
		return Object{}, classify(closeErr)
	}
	return object, nil
}

func DocumentKey(tenant, run, artifact uuid.UUID) (string, error) {
	if tenant == uuid.Nil || run == uuid.Nil || artifact == uuid.Nil {
		return "", ErrInvalid
	}
	return fmt.Sprintf("tenants/%s/runs/%s/documents/%s", tenant, run, artifact), nil
}

// PurgeDocument requires a DB cleanup ticket that permanently prevents this
// reservation from becoming ready. It accepts IDs, never a user-supplied key.
func (s *Store) PurgeDocument(ctx context.Context, tenant, run, artifact uuid.UUID) error {
	key, err := DocumentKey(tenant, run, artifact)
	if err != nil {
		return err
	}
	return s.purgeReservedKey(ctx, key)
}

// V2DraftKey names only a server-reserved draft object, never a client path.
func V2DraftKey(tenant, artifact uuid.UUID) (string, error) {
	if tenant == uuid.Nil || artifact == uuid.Nil {
		return "", ErrInvalid
	}
	return fmt.Sprintf("tenants/%s/v2-drafts/%s", tenant, artifact), nil
}

// PurgeV2Draft requires a cleanup ticket that has fenced API completion.
func (s *Store) PurgeV2Draft(ctx context.Context, tenant, artifact uuid.UUID) error {
	key, err := V2DraftKey(tenant, artifact)
	if err != nil {
		return err
	}
	return s.purgeReservedKey(ctx, key)
}
