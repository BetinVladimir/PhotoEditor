package provenance

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Verify the source/image arrays against the exact WorkingImage documents
// witnessed by the typed closure walk. RGB accepts finite HDR values.
func verifyDelegatedRenderWorkingArrays(ctx context.Context, catalog v2draft.ArtifactCatalog,
	objects SigningObjects, tenant uuid.UUID, inventory v2draft.TaskInputEvidence) error {
	if err := ctx.Err(); err != nil { return err }
	images := map[v2input.Ref]review.WorkingImage{}
	bodies := map[v2input.Ref]string{}
	for _, document := range inventory.Documents {
		if document.Contract != "WorkingImage" { continue }
		if body, exists := bodies[document.Ref]; exists {
			if body != document.JCS { return ErrInvalid }
			continue
		}
		image, err := review.ParseWorkingImage([]byte(document.JCS))
		if err != nil { return ErrInvalid }
		images[document.Ref] = image
		bodies[document.Ref] = document.JCS
	}
	for _, occurrence := range inventory.Occurrences {
		node := occurrence.Node
		if node.Contract != "WorkingPixels" && node.Contract != "WorkingAlpha" { continue }
		image, ok := images[node.ParentRef]
		if !ok { return ErrInvalid }
		width, height := image.Canvas.Width, image.Canvas.Height
		if width < 1 || height < 1 || width > 200000000 || height > 200000000/width {
			return ErrInvalid
		}
		channels := int64(3)
		bytesPerComponent := int64(4)
		maxBytesPerComponent := int64(4)
		if node.Contract == "WorkingPixels" {
			if node.Ref.ArtifactID != image.Pixels.ID || node.Ref.SHA256 != image.Pixels.SHA256 { return ErrInvalid }
			if image.DType == "uint16" { bytesPerComponent = 2 }
		} else {
			channels = 1
			bytesPerComponent = 2
			if image.Alpha == nil || node.Ref.ArtifactID != image.Alpha.ID || node.Ref.SHA256 != image.Alpha.SHA256 { return ErrInvalid }
		}
		object := occurrence.Object
		size := bytesPerComponent * width * height * channels
		if object.Key == "" || object.VersionID == "" || object.VersionID == "null" ||
			object.SHA256 != node.Ref.SHA256 || object.ContentType != "application/x-npy" ||
			object.Bytes < size || object.Bytes >
				maxBytesPerComponent*width*height*channels+65536 { return ErrInvalid }
		current, err := catalog.Lookup(ctx, auth.Identity{}, tenant, node)
		if err != nil { return err }
		if current != object { return ErrInvalid }
		stream, err := objects.Open(ctx, object)
		if err != nil { if stream != nil { _ = stream.Close() }; return err }
		if stream == nil { return ErrInvalid }
		if channels == 1 {
			err = masknpy.VerifyAlphaNPY(ctx, stream, object.Bytes,
				object.SHA256, width, height)
		} else if image.DType == "uint16" {
			err = masknpy.VerifyUint16RGBNPY(ctx, stream, object.Bytes,
				object.SHA256, width, height)
		} else {
			err = masknpy.VerifyFloat32ArrayNPY(ctx, stream, object.Bytes,
				object.SHA256, width, height, 3, false)
		}
		closeErr := stream.Close()
		if stopped := ctx.Err(); stopped != nil { return stopped }
		if err != nil { return err }
		if closeErr != nil { return closeErr }
		current, err = catalog.Lookup(ctx, auth.Identity{}, tenant, node)
		if err != nil { return err }
		if current != object { return ErrInvalid }
	}
	return ctx.Err()
}

