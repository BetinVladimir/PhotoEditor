package objectstore

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"
)

func OriginalKey(tenant, artifact uuid.UUID) (string, error) {
	if tenant == uuid.Nil || artifact == uuid.Nil {
		return "", ErrInvalid
	}
	return fmt.Sprintf("originals/tenants/%s/%s", tenant, artifact), nil
}

type originalStorage interface {
	Open(context.Context, Object) (io.ReadCloser, error)
	PutImmutable(context.Context, string, io.Reader, int64, string, string) (Object, error)
	RecoverImmutable(context.Context, string, int64, string, string) (Object, error)
}

// CopyOriginal streams a version-pinned verified source to a generated immutable
// originals key. Caller must own a durable target reservation and have reserved
// temporary double-storage headroom. This does not adopt an asset, consume quota
// or remove staging; those happen only after the returned version is committed.
func (s *Store) CopyOriginal(ctx context.Context, tenant, artifact uuid.UUID, source Object) (Object, error) {
	return copyOriginal(ctx, s, tenant, artifact, source)
}
func copyOriginal(ctx context.Context, store originalStorage, tenant, artifact uuid.UUID, source Object) (Object, error) {
	key, err := OriginalKey(tenant, artifact)
	if err != nil || store == nil || !source.valid() || source.Bytes < 1 || source.Bytes > 536870912 || source.Key == key {
		return Object{}, ErrInvalid
	}
	if err = ctx.Err(); err != nil {
		return Object{}, err
	}
	reader, err := store.Open(ctx, source)
	if err != nil {
		return Object{}, err
	}
	target, putErr := store.PutImmutable(ctx, key, reader, source.Bytes, source.SHA256, source.ContentType)
	closeErr := reader.Close()
	if putErr != nil {
		// A lost PUT response or an existing reserved key is resolved by verifying
		// all target bytes. Deterministic input/integrity failures are not masked.
		if errors.Is(putErr, ErrExists) || errors.Is(putErr, ErrUnavailable) {
			target, err = store.RecoverImmutable(ctx, key, source.Bytes, source.SHA256, source.ContentType)
			if err != nil {
				return Object{}, err
			}
		} else {
			return Object{}, putErr
		}
	}
	if closeErr != nil {
		return Object{}, closeErr
	}
	if !target.valid() || target.Key != key || target.Bytes != source.Bytes || target.SHA256 != source.SHA256 || target.ContentType != source.ContentType {
		return Object{}, ErrIntegrity
	}
	return target, nil
}

// PurgeOriginal requires a durable cleanup ticket. The DB either proves that a
// pending reservation has no artifact or fences a cancelled orphan artifact as
// deleting before issuing the ticket. Generated keys are unique per artifact.
func (s *Store) PurgeOriginal(ctx context.Context, tenant, artifact uuid.UUID) error {
	key, err := OriginalKey(tenant, artifact)
	if err != nil {
		return err
	}
	return s.purgeReservedKey(ctx, key)
}
