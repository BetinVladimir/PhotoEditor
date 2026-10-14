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

// A MaskRef parent fixes the exact float32 probability NPY canvas. Other
// MaskPixels parents are retained for their operation-specific validators.
func verifyDelegatedRenderMaskRefArrays(ctx context.Context,
	catalog v2draft.ArtifactCatalog, objects SigningObjects, tenant uuid.UUID,
	inventory v2draft.TaskInputEvidence) error {
	if err := ctx.Err(); err != nil { return err }
	masks := map[v2input.Ref]review.MaskRef{}
	bodies := map[v2input.Ref]string{}
	for _, document := range inventory.Documents {
		if document.Contract != "MaskRef" { continue }
		if body, exists := bodies[document.Ref]; exists {
			if body != document.JCS { return ErrInvalid }
			continue
		}
		mask, err := review.ParseMaskRef([]byte(document.JCS))
		if err != nil { return ErrInvalid }
		masks[document.Ref] = mask
		bodies[document.Ref] = document.JCS
	}
	for _, occurrence := range inventory.Occurrences {
		node := occurrence.Node
		if node.Contract != "MaskPixels" { continue }
		mask, found := masks[node.ParentRef]
		if !found { continue }
		width, height := mask.Canvas.Width, mask.Canvas.Height
		if width < 1 || height < 1 || width > 200000000 || height > 200000000/width ||
			node.Ref.ArtifactID != mask.Artifact.ID || node.Ref.SHA256 != mask.Artifact.SHA256 {
			return ErrInvalid
		}
		object := occurrence.Object
		size := 4 * width * height
		if object.Key == "" || object.VersionID == "" || object.VersionID == "null" ||
			object.SHA256 != node.Ref.SHA256 ||
			(object.ContentType != "application/x-npy" && object.ContentType != "application/octet-stream") ||
			object.Bytes < size || object.Bytes > size+65536 { return ErrInvalid }
		current, err := catalog.Lookup(ctx, auth.Identity{}, tenant, node)
		if err != nil { return err }
		if current != object { return ErrInvalid }
		stream, err := objects.Open(ctx, object)
		if err != nil { if stream != nil { _ = stream.Close() }; return err }
		if stream == nil { return ErrInvalid }
		err = masknpy.VerifyFloat32MaskNPY(ctx, stream, object.Bytes,
			object.SHA256, width, height)
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
