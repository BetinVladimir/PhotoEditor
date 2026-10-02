package v2render

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// VerifyCompositionMask verifies scoped pinned source/mask documents and the
// entire probability array. It closes every opened reader, including on early
// failure. It is a byte preflight; atomic admission must repeat live SQL scope.
func VerifyCompositionMask(ctx context.Context, who auth.Identity, tenant uuid.UUID, base v2input.SessionVersionBinding, ref v2input.Ref, catalog v2draft.ArtifactCatalog, objects v2input.SessionAnalysisObjectReader) (review.MaskRef, error) {
	var empty review.MaskRef
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if catalog == nil || objects == nil || tenant == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || base.AssetVersionID == uuid.Nil || !validLayerArtifactRef(base.Source) || !validLayerArtifactRef(ref) {
		return empty, v2input.ErrInvalid
	}
	source, err := readCompositionJSON(ctx, who, tenant, v2draft.LayerRef{Ref: base.Source, Contract: "WorkingImage", Path: "CompositionMask.source"}, catalog, objects, 8<<20)
	if err != nil {
		return empty, err
	}
	raw, err := readCompositionJSON(ctx, who, tenant, v2draft.LayerRef{Ref: ref, Contract: "MaskRef", Path: "CompositionMask.mask"}, catalog, objects, 1048576)
	if err != nil {
		return empty, err
	}
	mask, err := ValidateUserCompositionMask(raw, source, base)
	if err != nil {
		return empty, err
	}
	width, height := mask.Canvas.Width, mask.Canvas.Height
	if width < 1 || height < 1 || width > 200000000 || height > 200000000/width {
		return empty, v2input.ErrInvalid
	}
	pixels := v2input.Ref{ArtifactID: mask.Artifact.ID, SHA256: mask.Artifact.SHA256}
	object, err := catalog.Lookup(ctx, who, tenant, v2draft.LayerRef{Ref: pixels, Contract: "MaskPixels", ParentRef: ref, Path: "CompositionMask.mask.artifact"})
	if err != nil {
		return empty, err
	}
	dataBytes := width * height * 4
	if object.Bytes < dataBytes+10 || object.Bytes > dataBytes+65548 || object.SHA256 != pixels.SHA256 || object.Key == "" || object.VersionID == "" || object.VersionID == "null" || (object.ContentType != "application/x-npy" && object.ContentType != "application/octet-stream") {
		return empty, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	reader, err := objects.Open(ctx, object)
	if err != nil {
		return empty, err
	}
	if reader == nil {
		return empty, v2input.ErrInvalid
	}
	verifyErr := VerifyUserMaskNPY(ctx, reader, object.Bytes, pixels.SHA256, width, height)
	closeErr := reader.Close()
	if verifyErr != nil {
		return empty, verifyErr
	}
	if closeErr != nil {
		return empty, closeErr
	}
	return mask, ctx.Err()
}

// VerifyCompositionMasks checks every distinct expression/protected mask before
// any closure transport is published. Scope is repeated per pinned read; SQL
// admission must separately repeat the entire set's live relational authority.
func VerifyCompositionMasks(ctx context.Context, who auth.Identity, tenant uuid.UUID, compiled CompiledComposition, paramsRef v2input.Ref, catalog v2draft.ArtifactCatalog, objects v2input.SessionAnalysisObjectReader) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	roots, err := CompositionInputRoots(compiled, paramsRef)
	if err != nil {
		return err
	}
	for _, root := range roots {
		if root.Contract != "MaskRef" {
			continue
		}
		if _, err := VerifyCompositionMask(ctx, who, tenant, compiled.Params.Base, root.Ref, catalog, objects); err != nil {
			return err
		}
	}
	return ctx.Err()
}
