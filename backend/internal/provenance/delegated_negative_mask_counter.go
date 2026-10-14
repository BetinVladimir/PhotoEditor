package provenance

import (
 "context"

 "photoeditor/backend/internal/masknpy"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type DelegatedNegativeMaskCatalog interface {
 LookupDelegatedNegativeMask(context.Context, CanonicalSigningTask,
  v2input.Ref, review.MaskRef) (objectstore.Object, error)
}

type BoundDelegatedNegativeMaskCounter struct {
 Catalog DelegatedNegativeMaskCatalog
 Objects SigningObjects
}

func (s BoundDelegatedNegativeMaskCounter) CountDelegatedNegativeMask(ctx context.Context,
 task CanonicalSigningTask, descriptor v2input.Ref, mask review.MaskRef) (int64, error) {
 if err := ctx.Err(); err != nil { return 0, err }
 if s.Catalog == nil || s.Objects == nil || task.Operation != "optical.negative" ||
  task.Version != "1" || normalizeCanonicalSigningTask(&task) != nil ||
  !ref(descriptor) || !ref(v2input.Ref{ArtifactID: mask.Artifact.ID, SHA256: mask.Artifact.SHA256}) ||
  mask.Canvas.Width < 1 || mask.Canvas.Height < 1 || mask.Canvas.Width > 200000000 ||
  mask.Canvas.Height > 200000000/mask.Canvas.Width || mask.Protected ||
  mask.ObjectID != nil || mask.ModelLockID != nil || mask.Confidence != 1 ||
  mask.Selector != "changed" { return 0, ErrInvalid }
 owned, err := ownSigningProducerTask(task); if err != nil { return 0, err }
 object, err := s.Catalog.LookupDelegatedNegativeMask(ctx, owned, descriptor, mask)
 if err != nil { return 0, err }
 pixels := mask.Canvas.Width * mask.Canvas.Height
 if object.Key == "" || object.VersionID == "" || object.VersionID == "null" ||
  len(object.VersionID) > 1024 || object.SHA256 != mask.Artifact.SHA256 ||
  object.ContentType != "application/x-npy" && object.ContentType != "application/octet-stream" ||
  object.Bytes < 4*pixels || object.Bytes > 4*pixels+65536 { return 0, ErrInvalid }
 stream, err := s.Objects.Open(ctx, object)
 if err != nil { if stream != nil { _ = stream.Close() }; return 0, err }
 if stream == nil { return 0, ErrInvalid }
 count, readErr := masknpy.CountBinaryMaskNPY(ctx, stream, object.Bytes,
  object.SHA256, mask.Canvas.Width, mask.Canvas.Height)
 closeErr := stream.Close()
 if err := ctx.Err(); err != nil { return 0, err }
 if readErr != nil { return 0, readErr }
 if closeErr != nil { return 0, closeErr }
 current, err := s.Catalog.LookupDelegatedNegativeMask(ctx, owned, descriptor, mask)
 if err != nil { return 0, err }
 if current != object { return 0, ErrInvalid }
 return count, ctx.Err()
}

var _ DelegatedNegativeMaskCounter = BoundDelegatedNegativeMaskCounter{}
