package delivery

import (
 "bytes"
 "context"
 "encoding/json"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/provenance"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type DelegatedNegativePixelsCatalog struct {
 Tasks SQLDelegatedTaskCatalog
 Documents DelegatedTaskDocumentReader
 Adopted DelegatedAdoptedResultReader
}

func (s DelegatedNegativePixelsCatalog) LookupDelegatedNegativePixels(ctx context.Context,
 task provenance.CanonicalSigningTask, descriptor v2input.Ref,
 expected review.WorkingImage) (objectstore.Object, error) {
 var zero objectstore.Object
 if err := ctx.Err(); err != nil { return zero, err }
 if s.Tasks.DB == nil || s.Documents.Objects == nil ||
  s.Documents.Tasks.DB != s.Tasks.DB || s.Adopted.Tasks.DB != s.Tasks.DB ||
  s.Documents.Tasks.Lease != s.Tasks.Lease || s.Adopted.Tasks.Lease != s.Tasks.Lease ||
  s.Documents.Tasks.Scope != s.Tasks.Scope || s.Adopted.Tasks.Scope != s.Tasks.Scope ||
  s.Documents.Tasks.Ready != s.Tasks.Ready || s.Adopted.Tasks.Ready != s.Tasks.Ready ||
  s.Documents.Tasks.Slot.Ordinal != s.Tasks.Slot.Ordinal ||
  s.Adopted.Tasks.Slot.Ordinal != s.Tasks.Slot.Ordinal ||
  s.Documents.Tasks.Slot.PresetID != s.Tasks.Slot.PresetID ||
  s.Adopted.Tasks.Slot.PresetID != s.Tasks.Slot.PresetID ||
  s.Documents.Tasks.Bucket != s.Tasks.Bucket || s.Adopted.Tasks.Bucket != s.Tasks.Bucket ||
  task.Task == uuid.Nil || task.Operation != "optical.negative" || task.Version != "1" ||
  descriptor.ArtifactID == uuid.Nil || !shaPattern.MatchString(descriptor.SHA256) {
  return zero, v2input.ErrInvalid
 }
 root, result, err := s.Adopted.ReadDelegatedAdoptedResult(ctx, task)
 if err != nil { return zero, err }
 if result.WorkingImage != descriptor { return zero, v2input.ErrInvalid }
 raw, err := s.Documents.ReadDelegatedTaskDocument(ctx, task, descriptor, 1<<20)
 if err != nil { return zero, err }
 image, err := review.ParseWorkingImage(raw)
 if err != nil || image.DType != "float32" { return zero, v2input.ErrInvalid }
 actualRaw, err := canonical.Marshal(image)
 if err != nil { return zero, err }
 expectedRaw, err := canonical.Marshal(expected)
 if err != nil || !bytes.Equal(actualRaw, expectedRaw) ||
  image.Canvas.Width < 1 || image.Canvas.Height < 1 ||
  image.Canvas.Width > 200000000 ||
  image.Canvas.Height > 200000000/image.Canvas.Width { return zero, v2input.ErrInvalid }
 var wireRaw []byte
 minimum := 12*image.Canvas.Width*image.Canvas.Height
 err = s.Tasks.DB.QueryRow(ctx, `SELECT photo.v31_delivery_negative_pixels_object(
  $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
  s.Tasks.Lease.Expansion, s.Tasks.Lease.Token, s.Tasks.Slot.Ordinal,
  s.Tasks.Slot.PresetID, task.Task, root.ArtifactID, root.SHA256,
  descriptor.ArtifactID, descriptor.SHA256, image.Pixels.ID,
  image.Pixels.SHA256, s.Tasks.Bucket, minimum).Scan(&wireRaw)
 if err != nil { return zero, err }
 var wire struct {
  Bucket string `json:"bucket"`
  Key string `json:"key"`
  Version string `json:"version"`
  SHA256 string `json:"sha256"`
  Bytes int64 `json:"bytes"`
  MediaType string `json:"media_type"`
 }
 if len(wireRaw)<2 || len(wireRaw)>4096 || json.Unmarshal(wireRaw,&wire)!=nil ||
  wire.Bucket!=s.Tasks.Bucket || wire.Key=="" || wire.Version=="" ||
  wire.Version=="null" || wire.SHA256!=image.Pixels.SHA256 ||
  wire.Bytes<minimum || wire.Bytes>minimum+65536 ||
  wire.MediaType!="application/x-npy" { return zero,v2input.ErrInvalid }
 repeatedRoot, repeatedResult, err := s.Adopted.ReadDelegatedAdoptedResult(ctx, task)
 if err != nil { return zero, err }
 if repeatedRoot != root || repeatedResult.WorkingImage != descriptor ||
  repeatedResult.ChangedMask != result.ChangedMask { return zero,v2input.ErrInvalid }
 if err := ctx.Err(); err != nil { return zero, err }
 return objectstore.Object{Key:wire.Key,VersionID:wire.Version,
  SHA256:wire.SHA256,Bytes:wire.Bytes,ContentType:wire.MediaType},nil
}

var _ provenance.DelegatedNegativePixelsCatalog = DelegatedNegativePixelsCatalog{}
