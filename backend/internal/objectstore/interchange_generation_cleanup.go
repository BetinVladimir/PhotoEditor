package objectstore

import (
 "context"

 "github.com/google/uuid"
 "github.com/minio/minio-go/v7"
)

// DiscoverInterchangeGenerationMultipart is an observation only. An empty
// result never proves that a timed-out Create request has terminated.
func (s *Store) DiscoverInterchangeGenerationMultipart(ctx context.Context, tenant, upload, generation uuid.UUID) (string, error) {
 key, err := InterchangeGenerationKey(tenant, upload, generation)
 if err != nil { return "", err }
 if s == nil || s.client == nil || s.bucket == "" { return "", ErrInvalid }
 return discoverExactMultipart(ctx, key, func(scan context.Context) <-chan minio.ObjectMultipartInfo {
  return s.client.ListIncompleteUploads(scan, s.bucket, key, true)
 })
}

// DrainInterchangeGenerationMultipart requires a fenced writer and a known
// multipart ID. It cannot reconcile an unknown in-flight Create by itself.
func (s *Store) DrainInterchangeGenerationMultipart(ctx context.Context, tenant, upload, generation uuid.UUID, multipart string) error {
 key, err := InterchangeGenerationKey(tenant, upload, generation)
 if err != nil || !ValidInterchangeMultipartID(multipart) { return ErrInvalid }
 if s == nil || s.client == nil || s.bucket == "" { return ErrInvalid }
 core := minio.Core{Client:s.client}
 return drainExactGenerationMultipart(ctx, key, multipart,
  func(call context.Context) error { return core.AbortMultipartUpload(call, s.bucket, key, multipart) },
  func(call context.Context) error { _, err := core.ListObjectParts(call, s.bucket, key, multipart, 0, 1); return err },
  func(scan context.Context) <-chan minio.ObjectMultipartInfo { return s.client.ListIncompleteUploads(scan, s.bucket, key, true) })
}

func drainExactGenerationMultipart(ctx context.Context, key, multipart string, abort, probe func(context.Context) error, list func(context.Context) <-chan minio.ObjectMultipartInfo) error {
 if _, _, _, err := ParseInterchangeGenerationKey(key); err != nil || !ValidInterchangeMultipartID(multipart) || abort == nil || probe == nil || list == nil { return ErrInvalid }
 if err := ctx.Err(); err != nil { return err }
 if err := abort(ctx); err != nil && minio.ToErrorResponse(err).Code != "NoSuchUpload" { return classify(err) }
 if err := ctx.Err(); err != nil { return err }
 err := probe(ctx)
 if err == nil { return ErrUnavailable }
 if minio.ToErrorResponse(err).Code != "NoSuchUpload" { return classify(err) }
 if err := ctx.Err(); err != nil { return err }
 scan, stop := context.WithCancel(ctx); defer stop()
 rows := list(scan)
 if rows == nil { return ErrUnavailable }
 return awaitExactMultipartAbsent(ctx, key, rows)
}

// PurgeInterchangeGeneration is only for a generation whose Create call is
// terminal and whose SQL cleanup lease fences writers, signed parts and
// canonical artifacts. An empty listing alone is never a Create terminal proof.
func (s *Store) PurgeInterchangeGeneration(ctx context.Context, tenant, upload, generation uuid.UUID) error {
 key, err := InterchangeGenerationKey(tenant, upload, generation)
 if err != nil { return err }
 if s == nil || s.client == nil || s.bucket == "" { return ErrInvalid }
 return purgeExactInterchangeKey(ctx,key,s.purgeReservedKey,func(scan context.Context,key string) <-chan minio.ObjectMultipartInfo {
  return s.client.ListIncompleteUploads(scan,s.bucket,key,true)
 })
}

// PurgeInterchangeArchiveKey routes only a canonical key for the supplied
// tenant/upload. The caller must first hold the matching SQL cleanup lease.
func (s *Store) PurgeInterchangeArchiveKey(ctx context.Context, tenant, upload uuid.UUID, key string) error {
 legacy, err := InterchangeUploadKey(tenant,upload)
 if err != nil { return err }
 if key == legacy { return s.PurgeInterchangeUpload(ctx,tenant,upload) }
 gotTenant,gotUpload,generation,err := ParseInterchangeGenerationKey(key)
 if err != nil || gotTenant != tenant || gotUpload != upload { return ErrInvalid }
 return s.PurgeInterchangeGeneration(ctx,tenant,upload,generation)
}
