package v2portrait

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// ResolveRelightManualPlan binds original producer documents to the selected
// relight mask. Live producer authority and raster pixel replay remain required.
func ResolveRelightManualPlan(ctx context.Context, params RelightParams, sourceRaw, maskRaw, originalRaw, rootRaw, vectorsRaw []byte, originalRef, rootRef v2input.Ref, protected bool) (review.ManualRasterPlan, error) {
	var zero review.ManualRasterPlan
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if _, _, err := params.Compile(); err != nil {
		return zero, err
	}
	source, err := VerifyDonorWorkingImage(params.Base, sourceRaw)
	if err != nil {
		return zero, err
	}
	selected := params.Subject
	if protected {
		selected = params.Protected
	}
	sum := sha256.Sum256(maskRaw)
	if hex.EncodeToString(sum[:]) != selected.SHA256 {
		return zero, v2input.ErrInvalid
	}
	mask, err := review.ParseMaskRef(maskRaw)
	if err != nil {
		return zero, err
	}
	normalized, err := canonical.Marshal(mask)
	if err != nil || !bytes.Equal(normalized, maskRaw) || mask.Selector != "manual" || mask.Protected != protected {
		return zero, v2input.ErrInvalid
	}
	ref := func(r v2input.Ref) review.ArtifactRef { return review.ArtifactRef{ID: r.ArtifactID, SHA256: r.SHA256} }
	root, err := review.ParseManualRasterDocument(rootRaw, ref(rootRef))
	if err != nil {
		return zero, err
	}
	selectedMask := root.Target
	if protected {
		selectedMask = root.Protected
	}
	declared, err := canonical.Marshal(selectedMask)
	if err != nil || !bytes.Equal(declared, maskRaw) {
		return zero, v2input.ErrInvalid
	}
	// The shared verifier validates the whole original pair via its target.
	expected := root.Target
	return review.ResolveManualRasterPlan(ctx, originalRaw, ref(originalRef), rootRaw, ref(rootRef), expected,
		review.DefectSource{Image: ref(params.Base.Source), Pixels: source.Pixels, Canvas: source.Canvas, TransformChain: source.TransformChain}, vectorsRaw)
}
