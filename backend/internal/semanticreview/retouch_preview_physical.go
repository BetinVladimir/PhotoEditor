package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// PhysicalRetouchPreview contains a complete version-pinned edit/display set.
// It does not represent photographer acceptance, photographic qualification or Apply.
type PhysicalRetouchPreview struct {
	image          RetouchPreviewImageRead
	linkage        RetouchPreviewPresentationLinkage
	verified       bool
	adapterContext v2input.Ref
}

func ReadPhysicalRetouchPreview(ctx context.Context, db previewOutputQueryer, store planObjectStore,
	bucket string, who auth.Identity, run uuid.UUID, params RetouchPreviewParams, adapterContext v2input.Ref, sourceRaw []byte,
	iccLockSHA, srgbSHA string) (PhysicalRetouchPreview, error) {
	empty := PhysicalRetouchPreview{}
	image, linkage, err := ReadRetouchPresentationImage(ctx, db, store, bucket, who, run, params, adapterContext, sourceRaw, iccLockSHA, srgbSHA)
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
	latest, err := ReadRetouchPreviewPresentationLinkage(ctx, db, store, bucket, who, run, params, adapterContext, iccLockSHA, srgbSHA)
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
	return PhysicalRetouchPreview{image: image, linkage: linkage, verified: true, adapterContext: adapterContext}, nil
}
