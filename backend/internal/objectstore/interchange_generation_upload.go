package objectstore

import (
 "context"
 "fmt"
 "time"

 "github.com/google/uuid"
)

// BeginInterchangeGenerationMultipart must be called only after the caller's
// generation writer has been durably admitted. One invocation still permits
// only one non-idempotent S3 Create, even if the SDK attempts a retry.
func (s *Store) BeginInterchangeGenerationMultipart(ctx context.Context, tenant, upload, generation uuid.UUID) (string, error) {
 if err := ctx.Err(); err != nil { return "", fmt.Errorf("%w: %w",ErrInterchangeCreateNotDispatched,err) }
 key, err := InterchangeGenerationKey(tenant, upload, generation)
 if err != nil { return "", err }
 guarded,attempt:=trackedInterchangeCreateContext(ctx)
 id, err := s.BeginMultipart(guarded, key, "application/zip")
 if err != nil {
  if !attempt.Load() { return "",fmt.Errorf("%w: %w",ErrInterchangeCreateNotDispatched,err) }
  return "",err
 }
 if !ValidInterchangeMultipartID(id) { return "", ErrIntegrity }
 return id, nil
}

// SignInterchangeGenerationParts accepts the exact generation identity pinned
// by the SQL upload ledger, never a client-supplied arbitrary object key.
func (s *Store) SignInterchangeGenerationParts(ctx context.Context, tenant, upload, generation uuid.UUID, multipart string, size int64, parts []int, notAfter time.Time) ([]PartURL, error) {
 if err := ctx.Err(); err != nil { return nil, err }
 key, err := InterchangeGenerationKey(tenant, upload, generation)
 if err != nil || !ValidInterchangeMultipartID(multipart) { return nil, ErrInvalid }
 now := time.Now().UTC()
 ttl, err := interchangePartsTTL(now, notAfter, size, parts)
 if err != nil { return nil, err }
 signing, stop := context.WithDeadline(ctx, notAfter); defer stop()
 urls, err := s.SignUploadParts(signing, key, multipart, parts, ttl)
 if err != nil { return nil, err }
 if err := signing.Err(); err != nil { return nil, err }
 for _, part := range urls { if part.ExpiresAt.After(notAfter) { return nil, ErrIntegrity } }
 return urls, nil
}

// AbortInterchangeGenerationMultipart aborts exactly one upload ID for the
// selected generation; it never drains another generation's key.
func (s *Store) AbortInterchangeGenerationMultipart(ctx context.Context, tenant, upload, generation uuid.UUID, multipart string) error {
 if err := ctx.Err(); err != nil { return err }
 key, err := InterchangeGenerationKey(tenant, upload, generation)
 if err != nil || !ValidInterchangeMultipartID(multipart) { return ErrInvalid }
 return s.AbortMultipart(ctx, key, multipart)
}

// CompleteInterchangeGenerationMultipart preserves the V30 size, part and
// immutable-version checks while binding the transport to one generation.
func (s *Store) CompleteInterchangeGenerationMultipart(ctx context.Context, tenant, upload, generation uuid.UUID, multipart string, size int64, parts []UploadedPart) (PendingObject, error) {
 if err := ctx.Err(); err != nil { return PendingObject{}, err }
 key, err := InterchangeGenerationKey(tenant, upload, generation)
 if err != nil || !ValidInterchangeMultipartID(multipart) || size < 22 { return PendingObject{}, ErrInvalid }
 return s.completeMultipartBounded(ctx, key, multipart, "application/zip", size, parts, MaxInterchangeArchiveBytes)
}

// RecoverInterchangeGenerationCompletion reads a completed immutable version
// after an uncertain Complete reply. Caller must independently prove that the
// exact multipart capability is absent before accepting the result.
func (s *Store) RecoverInterchangeGenerationCompletion(ctx context.Context, tenant, upload, generation uuid.UUID, size int64) (PendingObject, error) {
 if err := ctx.Err(); err != nil { return PendingObject{}, err }
 key, err := InterchangeGenerationKey(tenant, upload, generation)
 if err != nil || size < 22 { return PendingObject{}, ErrInvalid }
 return s.recoverCompletedMultipartBounded(ctx, key, "application/zip", size, MaxInterchangeArchiveBytes)
}

// VerifyInterchangeGenerationPending hashes the exact immutable version and
// refuses a pending object from any other generation or legacy upload key.
func (s *Store) VerifyInterchangeGenerationPending(ctx context.Context, tenant, upload, generation uuid.UUID, pending PendingObject, expectedSHA string) (Object, error) {
 if err := ctx.Err(); err != nil { return Object{}, err }
 key, err := InterchangeGenerationKey(tenant, upload, generation)
 if err != nil || pending.Key != key || pending.Bytes < 22 || pending.ContentType != "application/zip" || !ValidInterchangeVersionID(pending.VersionID) || !hashPattern.MatchString(expectedSHA) { return Object{}, ErrInvalid }
 return s.verifyPendingBounded(ctx, pending, expectedSHA, MaxInterchangeArchiveBytes)
}
