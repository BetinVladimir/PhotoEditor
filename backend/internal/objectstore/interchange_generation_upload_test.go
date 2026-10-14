package objectstore

import (
 "context"
 "errors"
 "strings"
 "testing"
 "time"

 "github.com/google/uuid"
)

func TestInterchangeGenerationTransportRefusesInvalidIdentityBeforeIO(t *testing.T) {
 var store *Store
 tenant, upload, generation := uuid.New(), uuid.New(), uuid.New()
 for _, ids := range [][3]uuid.UUID{{uuid.Nil, upload, generation}, {tenant, uuid.Nil, generation}, {tenant, upload, uuid.Nil}} {
  if _, err := store.BeginInterchangeGenerationMultipart(context.Background(), ids[0], ids[1], ids[2]); err != ErrInvalid { t.Fatal("Begin accepted invalid generation", err) }
  if _, err := store.SignInterchangeGenerationParts(context.Background(), ids[0], ids[1], ids[2], "multipart", 22, []int{1}, time.Now().Add(time.Hour)); err != ErrInvalid { t.Fatal("Parts accepted invalid generation", err) }
  if err := store.AbortInterchangeGenerationMultipart(context.Background(), ids[0], ids[1], ids[2], "multipart"); err != ErrInvalid { t.Fatal("Abort accepted invalid generation", err) }
  if _, err := store.CompleteInterchangeGenerationMultipart(context.Background(), ids[0], ids[1], ids[2], "multipart", 22, []UploadedPart{{PartNumber:1,ETag:"etag"}}); err != ErrInvalid { t.Fatal("Complete accepted invalid generation", err) }
  if _, err := store.RecoverInterchangeGenerationCompletion(context.Background(), ids[0], ids[1], ids[2], 22); err != ErrInvalid { t.Fatal("Recover accepted invalid generation", err) }
 }
 if _, err := store.SignInterchangeGenerationParts(context.Background(), tenant, upload, generation, "", 22, []int{1}, time.Now().Add(time.Hour)); err != ErrInvalid { t.Fatal("Parts accepted empty multipart", err) }
 if err := store.AbortInterchangeGenerationMultipart(context.Background(), tenant, upload, generation, ""); err != ErrInvalid { t.Fatal("Abort accepted empty multipart", err) }
 key, _ := InterchangeGenerationKey(tenant, upload, generation)
 pending := PendingObject{Key:key,VersionID:"version",ContentType:"application/zip",Bytes:22}
 if _, err := store.VerifyInterchangeGenerationPending(context.Background(), tenant, upload, uuid.New(), pending, strings.Repeat("a",64)); err != ErrInvalid { t.Fatal("Verify accepted another generation", err) }
 legacy, _ := InterchangeUploadKey(tenant, upload); pending.Key = legacy
 if _, err := store.VerifyInterchangeGenerationPending(context.Background(), tenant, upload, generation, pending, strings.Repeat("a",64)); err != ErrInvalid { t.Fatal("Verify accepted legacy key", err) }
}

func TestInterchangeGenerationTransportStopsBeforeIOOnCancellation(t *testing.T) {
 ctx, cancel := context.WithCancel(context.Background()); cancel()
 var store *Store
 tenant, upload, generation := uuid.New(), uuid.New(), uuid.New()
 if _, err := store.BeginInterchangeGenerationMultipart(ctx, tenant, upload, generation); !errors.Is(err,context.Canceled)||!errors.Is(err,ErrInterchangeCreateNotDispatched) { t.Fatal(err) }
 if _, err := store.SignInterchangeGenerationParts(ctx, tenant, upload, generation, "multipart", 22, []int{1}, time.Now().Add(time.Hour)); err != context.Canceled { t.Fatal(err) }
 if err := store.AbortInterchangeGenerationMultipart(ctx, tenant, upload, generation, "multipart"); err != context.Canceled { t.Fatal(err) }
 if _, err := store.CompleteInterchangeGenerationMultipart(ctx, tenant, upload, generation, "multipart", 22, nil); err != context.Canceled { t.Fatal(err) }
 if _, err := store.RecoverInterchangeGenerationCompletion(ctx, tenant, upload, generation, 22); err != context.Canceled { t.Fatal(err) }
 if _, err := store.VerifyInterchangeGenerationPending(ctx, tenant, upload, generation, PendingObject{}, ""); err != context.Canceled { t.Fatal(err) }
}
