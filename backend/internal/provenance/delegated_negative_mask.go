package provenance

import (
 "bytes"
 "context"

 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// ReadDelegatedNegativeMask binds the changed-mask descriptor to the exact
// output canvas and transport, while physical NPY counting remains mandatory.
func ReadDelegatedNegativeMask(ctx context.Context, documents DelegatedTaskDocuments,
 task CanonicalSigningTask, image review.WorkingImage, reference v2input.Ref) (review.MaskRef, error) {
 var zero review.MaskRef
 if err := ctx.Err(); err != nil { return zero, err }
 if documents == nil || task.Operation != "optical.negative" || task.Version != "1" ||
  normalizeCanonicalSigningTask(&task) != nil || !ref(reference) { return zero, ErrInvalid }
 owned, err := ownSigningProducerTask(task); if err != nil { return zero, err }
 imageRaw, err := canonical.Marshal(image)
 if err != nil { return zero, ErrInvalid }
 if _, err := review.ParseWorkingImage(imageRaw); err != nil { return zero, ErrInvalid }
 raw, err := documents.ReadDelegatedTaskDocument(ctx, owned, reference, 1<<20)
 if err != nil { return zero, err }
 if !pinned(raw, reference, 1<<20) { return zero, ErrInvalid }
 body := append([]byte(nil), raw...)
 mask, err := review.ParseMaskRef(body)
 if err != nil || mask.Canvas != image.Canvas ||
  !bytes.Equal(mask.TransformChain, image.TransformChain) ||
  mask.Selector != "changed" || mask.Protected || mask.ObjectID != nil ||
  mask.Confidence != 1 || mask.ModelLockID != nil { return zero, ErrInvalid }
 current, err := documents.ReadDelegatedTaskDocument(ctx, owned, reference, 1<<20)
 if err != nil { return zero, err }
 if !bytes.Equal(current, body) { return zero, ErrInvalid }
 return mask, ctx.Err()
}
