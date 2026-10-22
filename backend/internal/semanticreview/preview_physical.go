package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
)

// PhysicalExposurePreview contains a complete version-pinned edit/display set.
// It does not represent photographer acceptance, photographic qualification or Apply.
type PhysicalExposurePreview struct {
	image    PreviewImageRead
	linkage  PreviewPresentationLinkage
	verified bool
}

func ReadPhysicalExposurePreview(ctx context.Context, db previewOutputQueryer, store planObjectStore,
	bucket string, who auth.Identity, run uuid.UUID, params ExposurePreviewParams, sourceRaw []byte,
	iccLockSHA, srgbSHA string) (PhysicalExposurePreview, error) {
	empty := PhysicalExposurePreview{}
	image, linkage, err := ReadExposurePresentationImage(ctx, db, store, bucket, who, run, params, sourceRaw, iccLockSHA, srgbSHA)
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
	latest, err := ReadExposurePreviewPresentationLinkage(ctx, db, store, bucket, who, run, params, iccLockSHA, srgbSHA)
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
	return PhysicalExposurePreview{image: image, linkage: linkage, verified: true}, nil
}

func readAndVerifyPreviewPNG(ctx context.Context, store planObjectStore, bucket string, source PlanSource,
	width, height int, srgbSHA, role string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if store == nil || bucket == "" || source.Bucket != bucket || source.Ref.ArtifactID == uuid.Nil ||
		source.Key == "" || source.Version == "" || source.Version == "null" || source.MediaType != "image/png" ||
		source.Bytes < 33 || source.Bytes > 33882112 || !shaPattern.MatchString(source.Ref.SHA256) {
		return ErrInvalid
	}
	stream, err := store.Open(ctx, objectstore.Object{Key: source.Key, VersionID: source.Version,
		SHA256: source.Ref.SHA256, Bytes: source.Bytes, ContentType: source.MediaType})
	if err != nil {
		return err
	}
	if stream == nil {
		return ErrInvalid
	}
	// One PNG at a time: never retain the four decoded images or compressed buffers.
	raw, readErr := io.ReadAll(io.LimitReader(stream, source.Bytes+1))
	closeErr := stream.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if int64(len(raw)) != source.Bytes {
		return ErrInvalid
	}
	return VerifyExposurePreviewPNG(ctx, raw, source.Ref.SHA256, width, height, srgbSHA, role)
}
