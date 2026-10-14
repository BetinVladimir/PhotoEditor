package provenance

import (
	"bytes"
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// A validated descriptor is not proof of a nonempty generated mask. The binary
// NPY artifact still needs scoped immutable streaming verification and counting.
func ReadSigningNegativeMask(ctx context.Context, documents SigningTaskDocumentReader, who auth.Identity, ready ReadySigningExport, task CanonicalSigningTask, image review.WorkingImage, reference v2input.Ref, selector string) (review.MaskRef, error) {
	var zero review.MaskRef
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if documents == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || ready.Tenant == uuid.Nil || ready.Project == uuid.Nil || ready.ExportID == uuid.Nil || task.Operation != "optical.negative" || task.Version != "1" || normalizeCanonicalSigningTask(&task) != nil || !ref(reference) || selector != "changed" {
		return zero, ErrInvalid
	}
	imageRaw, err := canonical.Marshal(image)
	if err != nil {
		return zero, ErrInvalid
	}
	if _, err := review.ParseWorkingImage(imageRaw); err != nil {
		return zero, ErrInvalid
	}
	raw, err := documents.ReadSigningTaskDocument(ctx, who, ready, task, reference, 1<<20)
	if err != nil {
		return zero, err
	}
	if !pinned(raw, reference, 1<<20) {
		return zero, ErrInvalid
	}
	owned := append([]byte(nil), raw...)
	mask, err := review.ParseMaskRef(owned)
	if err != nil {
		return zero, ErrInvalid
	}
	if mask.Canvas != image.Canvas || !bytes.Equal(mask.TransformChain, image.TransformChain) || mask.Selector != selector || mask.Protected || mask.ObjectID != nil || mask.Confidence != 1 || mask.ModelLockID != nil {
		return zero, ErrInvalid
	}
	current, err := documents.ReadSigningTaskDocument(ctx, who, ready, task, reference, 1<<20)
	if err != nil {
		return zero, err
	}
	if !bytes.Equal(current, owned) {
		return zero, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return mask, nil
}
