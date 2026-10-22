package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// ReadBackgroundCleanupPresentationImage verifies full-resolution edit bytes under the
// presentation's canonical attempt. Display PNG qualification remains separate.
func ReadBackgroundCleanupPresentationImage(ctx context.Context, db previewOutputQueryer,
	store planObjectStore, bucket string, who auth.Identity, run uuid.UUID, params BackgroundCleanupPreviewParams,
	sourceRaw []byte, iccLockSHA, srgbSHA string) (BackgroundCleanupPreviewImageRead, BackgroundCleanupPreviewPresentationLinkage, error) {
	empty := BackgroundCleanupPreviewImageRead{}
	none := BackgroundCleanupPreviewPresentationLinkage{}
	if err := ctx.Err(); err != nil {
		return empty, none, err
	}
	if len(sourceRaw) < 2 || len(sourceRaw) > 1048576 {
		return empty, none, ErrInvalid
	}
	sourceRaw = append([]byte(nil), sourceRaw...)
	linkage, err := ReadBackgroundCleanupPreviewPresentationLinkage(ctx, db, store, bucket, who, run, params, iccLockSHA, srgbSHA)
	if err != nil {
		return empty, none, err
	}
	read, err := NewPlanObjectReader(store, bucket)
	if err != nil {
		return empty, none, err
	}
	image, pixels := linkage.Sidecars[1], linkage.Sidecars[2]
	raw, err := read(ctx, image)
	if err != nil {
		return empty, none, err
	}
	if int64(len(raw)) != image.Bytes {
		return empty, none, ErrInvalid
	}
	metadata, err := VerifyBackgroundCleanupPreviewImageBinding(linkage.Evidence.Evidence, sourceRaw, raw)
	if err != nil {
		return empty, none, err
	}
	// Preview edge rounding is specified as nearest-even, with no upscale.
	width, height := previewDisplayDimensions(metadata.Width, metadata.Height)
	if linkage.Presentation.Width != width || linkage.Presentation.Height != height {
		return empty, none, ErrInvalid
	}
	stream, err := store.Open(ctx, objectstore.Object{Key: pixels.Key, VersionID: pixels.Version,
		SHA256: pixels.Ref.SHA256, Bytes: pixels.Bytes, ContentType: pixels.MediaType})
	if err != nil {
		return empty, none, err
	}
	if stream == nil {
		return empty, none, ErrInvalid
	}
	verifyErr := masknpy.VerifyNonnegativeFloat32RGBNPY(ctx, stream, pixels.Bytes, pixels.Ref.SHA256, metadata.Width, metadata.Height)
	closeErr := stream.Close()
	if verifyErr != nil {
		return empty, none, verifyErr
	}
	if closeErr != nil {
		return empty, none, closeErr
	}
	current, err := ReadBackgroundCleanupPreviewPresentationLinkage(ctx, db, store, bucket, who, run, params, iccLockSHA, srgbSHA)
	if err != nil {
		return empty, none, err
	}
	if err := ctx.Err(); err != nil {
		return empty, none, err
	}
	if current.Sidecars != linkage.Sidecars || current.Document.Source != linkage.Document.Source ||
		current.Document.ResultID != linkage.Document.ResultID || current.Document.TaskID != linkage.Document.TaskID ||
		current.Document.Attempt != linkage.Document.Attempt || current.Document.Fence != linkage.Document.Fence {
		return empty, none, ErrConflict
	}
	return BackgroundCleanupPreviewImageRead{Evidence: linkage.Evidence, Image: image, Pixels: pixels, Metadata: metadata,
		ImageBytes: append([]byte(nil), raw...)}, linkage, nil
}

// BackgroundCleanupPreviewImageRead verifies physical RGB bytes and metadata, not
// independent photographic quality or display PNG reconstruction.
type BackgroundCleanupPreviewImageRead struct {
	Evidence      VerifiedBackgroundCleanupPreviewEvidence
	Image, Pixels PlanSource
	Metadata      v2input.SemanticSourceMetadata
	ImageBytes    []byte
}
