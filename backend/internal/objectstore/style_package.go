package objectstore

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"strings"
	"sync/atomic"
)

func StylePackageKey(tenant, upload uuid.UUID) (string, error) {
	if tenant == uuid.Nil || upload == uuid.Nil {
		return "", ErrInvalid
	}
	return "tenants/" + tenant.String() + "/style-package-uploads/" + upload.String() + "/package", nil
}

// Caller must hold a durable exclusive cleanup ticket and fence all IO/signers.
func (s *Store) PurgeStylePackage(ctx context.Context, tenant, upload uuid.UUID) error {
	key, e := StylePackageKey(tenant, upload)
	if e != nil {
		return e
	}
	if s == nil || s.client == nil || s.bucket == "" {
		return ErrInvalid
	}
	return purgeExactInterchangeKey(ctx, key, s.purgeReservedKey, func(ctx context.Context, key string) <-chan minio.ObjectMultipartInfo {
		return s.client.ListIncompleteUploads(ctx, s.bucket, key, true)
	})
}

// A discovered ID witnesses completion of the single durable Create attempt.
// Empty listing is NOT a terminal outcome and must never release quota.
func (s *Store) DiscoverStylePackageMultipart(ctx context.Context, tenant, upload uuid.UUID) (string, error) {
	key, e := StylePackageKey(tenant, upload)
	if e != nil {
		return "", e
	}
	if s == nil || s.client == nil || s.bucket == "" {
		return "", ErrInvalid
	}
	return discoverExactMultipart(ctx, key, func(scan context.Context) <-chan minio.ObjectMultipartInfo {
		return s.client.ListIncompleteUploads(scan, s.bucket, key, true)
	})
}

var ErrStylePackageCreateNotCreated = errors.New("style package multipart create definitively not created")

type stylePackageCreateOutcomeKey struct{}
type stylePackageCreateOutcome struct{ rejected atomic.Bool }

func (o *stylePackageCreateOutcome) record(status int) {
	switch status {
	case 400, 401, 403, 404, 405, 411, 413, 415, 422:
		o.rejected.Store(true)
	}
}

// Exactly one physical Create. Only pre-dispatch or an explicit non-creating
// response may close an empty writer. Timeout/5xx/empty listing is never proof.
func (s *Store) BeginStylePackageMultipart(ctx context.Context, tenant, upload uuid.UUID, media string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("%w: %w", ErrStylePackageCreateNotCreated, err)
	}
	key, err := StylePackageKey(tenant, upload)
	if err != nil || s == nil || s.client == nil || (media != "application/json" && media != "application/zip") {
		return "", fmt.Errorf("%w: %w", ErrStylePackageCreateNotCreated, ErrInvalid)
	}
	outcome := new(stylePackageCreateOutcome)
	guarded, attempt := trackedInterchangeCreateContext(context.WithValue(ctx, stylePackageCreateOutcomeKey{}, outcome))
	id, err := s.BeginMultipart(guarded, key, media)
	if err != nil {
		if !attempt.Load() || outcome.rejected.Load() {
			return "", fmt.Errorf("%w: %w", ErrStylePackageCreateNotCreated, err)
		}
		return "", err
	}
	return id, nil
}

// Missing digest metadata is expected only for these exact multipart namespaces.
// The caller still verifies the entire pinned version before returning any bytes.
func allowsMultipartDigestVerification(object Object) bool {
	if isMultipartSourceKey(object.Key) {
		return object.Bytes >= 1 && object.Bytes <= MaxUploadBytes
	}
	if !isStylePackageSourceKey(object.Key) {
		return false
	}
	return object.Bytes >= 1 && object.Bytes <= 100*1024*1024 && (object.ContentType == "application/json" || object.ContentType == "application/zip")
}
func isStylePackageSourceKey(key string) bool {
	parts := strings.Split(key, "/")
	if len(parts) != 5 || parts[0] != "tenants" || parts[2] != "style-package-uploads" || parts[4] != "package" {
		return false
	}
	tenant, e := uuid.Parse(parts[1])
	if e != nil {
		return false
	}
	upload, e := uuid.Parse(parts[3])
	if e != nil {
		return false
	}
	canonical, e := StylePackageKey(tenant, upload)
	return e == nil && key == canonical
}
