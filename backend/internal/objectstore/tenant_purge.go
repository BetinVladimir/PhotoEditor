package objectstore

import (
	"context"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"strings"
)

func tenantNamespaces(tenant uuid.UUID) ([]string, error) {
	prefix, err := tenantPrefix(tenant)
	if err != nil {
		return nil, err
	}
	return []string{prefix, "staging/" + prefix, "originals/" + prefix}, nil
}

// S3 explicitly identifies objects predating versioning as the literal "null"
// version. Tenant-wide purge must remove that version rather than leave legacy
// data forever. Empty identifiers remain forbidden: omission is a different DELETE.
// Live immutable artifact readers/writers retain their stricter version policy.
func validTenantPurgeVersionID(version string) bool {
 return version == "null" || ValidInterchangeVersionID(version)
}

type tenantPurgeIO struct {
	versions      func(context.Context, string) <-chan minio.ObjectInfo
	uploads       func(context.Context, string) <-chan minio.ObjectMultipartInfo
	removeVersion func(context.Context, string, string) error
	removeUpload  func(context.Context, string, string) error
}

func purgeTenantBatch(ctx context.Context, tenant uuid.UUID, limit int, io tenantPurgeIO) (bool, error) {
	prefixes, err := tenantNamespaces(tenant)
	if err != nil || limit < 1 || limit > 256 || io.versions == nil || io.uploads == nil || io.removeVersion == nil || io.removeUpload == nil {
		return false, ErrInvalid
	}
	removed := 0
	uploads := func(prefix string, remove bool) (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		scan, cancel := context.WithCancel(ctx)
		defer cancel()
		rows := io.uploads(scan, prefix)
		if rows == nil {
			return false, ErrUnavailable
		}
		for {
			var row minio.ObjectMultipartInfo
			select {
			case <-ctx.Done():
				return false, ctx.Err()
			case observed, ok := <-rows:
				if !ok {
					return false, ctx.Err()
				}
				row = observed
			}
			if err := ctx.Err(); err != nil {
				return false, err
			}
			if row.Err != nil {
				return false, classify(row.Err)
			}
			// S3 keys are opaque. Legacy objects may not obey the grammar used
			// for new writes; the exact tenant prefix and upload ID are the
			// deletion boundary here.
			if !strings.HasPrefix(row.Key, prefix) || !ValidInterchangeMultipartID(row.UploadID) {
				return false, ErrInvalid
			}
			if !remove {
				return true, nil
			}
			if err := io.removeUpload(ctx, row.Key, row.UploadID); err != nil {
				return false, classify(err)
			}
			removed++
			if removed >= limit {
				return true, ctx.Err()
			}
		}
	}
	versions := func(prefix string, remove bool) (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		scan, cancel := context.WithCancel(ctx)
		defer cancel()
		rows := io.versions(scan, prefix)
		if rows == nil {
			return false, ErrUnavailable
		}
		for {
			var row minio.ObjectInfo
			select {
			case <-ctx.Done():
				return false, ctx.Err()
			case observed, ok := <-rows:
				if !ok {
					return false, ctx.Err()
				}
				row = observed
			}
			if err := ctx.Err(); err != nil {
				return false, err
			}
			if row.Err != nil {
				return false, classify(row.Err)
			}
			if !strings.HasPrefix(row.Key, prefix) || !validTenantPurgeVersionID(row.VersionID) {
				return false, ErrInvalid
			}
			if !remove {
				return true, nil
			}
			if err := io.removeVersion(ctx, row.Key, row.VersionID); err != nil {
				return false, classify(err)
			}
			removed++
			if removed >= limit {
				return true, ctx.Err()
			}
		}
	}
	for _, remove := range []bool{true, false} {
		for _, prefix := range prefixes {
			if more, err := uploads(prefix, remove); more || err != nil {
				return more, err
			}
			if more, err := versions(prefix, remove); more || err != nil {
				return more, err
			}
		}
	}
	return false, ctx.Err()
}
