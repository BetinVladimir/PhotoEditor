package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
)

// PhysicalBackgroundCleanupPreview contains a complete version-pinned edit/display set.
// It does not represent photographer acceptance, photographic qualification or Apply.
type PhysicalBackgroundCleanupPreview struct {
	image    BackgroundCleanupPreviewImageRead
	linkage  BackgroundCleanupPreviewPresentationLinkage
	verified bool
}

func ReadPhysicalBackgroundCleanupPreview(ctx context.Context, db previewOutputQueryer, store planObjectStore,
	bucket string, who auth.Identity, run uuid.UUID, params BackgroundCleanupPreviewParams, sourceRaw []byte,
	iccLockSHA, srgbSHA string) (PhysicalBackgroundCleanupPreview, error) {
	empty := PhysicalBackgroundCleanupPreview{}
	image, linkage, err := ReadBackgroundCleanupPresentationImage(ctx, db, store, bucket, who, run, params, sourceRaw, iccLockSHA, srgbSHA)
	if err != nil {
		return empty, err
	}
	for index, role := range []string{"before", "after", "affected", "protected"} {
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		source := linkage.Sidecars[index+3]
		if err := readAndVerifyPreviewPNG(ctx, store, bucket, source, linkage.Presentation.Width,
			linkage.Presentation.Height, srgbSHA, role); err != nil {
			return empty, err
		}
	}
	latest, err := ReadBackgroundCleanupPreviewPresentationLinkage(ctx, db, store, bucket, who, run, params, iccLockSHA, srgbSHA)
	if err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if latest.Sidecars != linkage.Sidecars || latest.Document.Source != linkage.Document.Source ||
		latest.Document.TaskID != linkage.Document.TaskID || latest.Document.ResultID != linkage.Document.ResultID ||
		latest.Document.Attempt != linkage.Document.Attempt || latest.Document.Fence != linkage.Document.Fence {
		return empty, ErrConflict
	}
	return PhysicalBackgroundCleanupPreview{image: image, linkage: linkage, verified: true}, nil
}
