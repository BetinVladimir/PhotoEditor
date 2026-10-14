package objectstore

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
)

var ErrPurgeIncomplete = errors.New("tenant purge storage not empty")

type RestorePurgeStorageEvidence struct {
	TenantID   uuid.UUID
	Bucket     string
	Prefixes   []string
	Passes     int
	ObservedAt time.Time
}

// ObservePurgedTenant is read-only. The installed guard must attest the exact
// isolated fixture tenant and paused writers. Every configured bucket requires
// its own observation; this method never infers complete deployment coverage.
func (s *Store) ObservePurgedTenant(ctx context.Context, tenant uuid.UUID,
	assertFixture func(context.Context, uuid.UUID) error) (RestorePurgeStorageEvidence, error) {
	if s == nil || s.client == nil || s.bucket == "" {
		return RestorePurgeStorageEvidence{}, ErrInvalid
	}
	return observePurgedTenant(ctx, s.bucket, tenant, assertFixture,
		func(ctx context.Context, prefix string) <-chan minio.ObjectInfo {
			return s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true, WithVersions: true})
		}, func(ctx context.Context, prefix string) <-chan minio.ObjectMultipartInfo {
			return s.client.ListIncompleteUploads(ctx, s.bucket, prefix, true)
		})
}

func observePurgedTenant(ctx context.Context, bucket string, tenant uuid.UUID,
	guard func(context.Context, uuid.UUID) error,
	versions func(context.Context, string) <-chan minio.ObjectInfo,
	uploads func(context.Context, string) <-chan minio.ObjectMultipartInfo) (RestorePurgeStorageEvidence, error) {
	empty := RestorePurgeStorageEvidence{}
	if tenant == uuid.Nil || bucket == "" || guard == nil || versions == nil || uploads == nil {
		return empty, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := guard(ctx, tenant); err != nil {
			return err
		}
		return ctx.Err()
	}
	prefixes, _ := tenantNamespaces(tenant)
	for pass := 0; pass < 2; pass++ {
		for _, prefix := range prefixes {
			if err := check(); err != nil {
				return empty, err
			}
			// Child cancellation releases SDK listing goroutines on early refusal.
			scan, stop := context.WithCancel(ctx)
			versionErr := func() error {
				defer stop()
				rows := versions(scan, prefix)
				if rows == nil {
					return ErrUnavailable
				}
				for {
					var item minio.ObjectInfo
					select {
					case <-ctx.Done():
						return ctx.Err()
					case observed, ok := <-rows:
						if !ok { return check() }
						item = observed
					}
					if item.Err != nil {
						return classify(item.Err)
					}
					return ErrPurgeIncomplete
				}
			}()
			if versionErr != nil {
				return empty, versionErr
			}
			scan, stop = context.WithCancel(ctx)
			uploadErr := func() error {
				defer stop()
				rows := uploads(scan, prefix)
				if rows == nil {
					return ErrUnavailable
				}
				for {
					var item minio.ObjectMultipartInfo
					select {
					case <-ctx.Done():
						return ctx.Err()
					case observed, ok := <-rows:
						if !ok { return check() }
						item = observed
					}
					if item.Err != nil {
						return classify(item.Err)
					}
					return ErrPurgeIncomplete
				}
			}()
			if uploadErr != nil {
				return empty, uploadErr
			}
		}
	}
	if err := check(); err != nil {
		return empty, err
	}
	return RestorePurgeStorageEvidence{tenant, bucket, prefixes, 2, time.Now().UTC()}, nil
}
