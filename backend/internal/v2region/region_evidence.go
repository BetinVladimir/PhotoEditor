package v2region

import (
	"crypto/sha256"
	"encoding/hex"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2learning"
)

type CompiledRegionEvidence struct {
	Geometry RegionGeometry
	Source   review.WorkingImage
	Recipe   v2learning.FrozenRecipe
}

// CompileRegionEvidence repeats immutable-byte and source binding checks.
// Caller still must prove scoped SQL authority and pin object versions/closure.
func CompileRegionEvidence(req RegionPreviewRequest, recipeRaw []byte, sourceRef v2input.Ref, sourceRaw []byte) (CompiledRegionEvidence, error) {
	var empty CompiledRegionEvidence
	hash := func(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
	if sourceRef.ArtifactID == [16]byte{} || req.VersionID == [16]byte{} || req.Recipe.ArtifactID == [16]byte{} || hash(recipeRaw) != req.Recipe.SHA256 || hash(sourceRaw) != sourceRef.SHA256 {
		return empty, ErrRegionInvalid
	}
	recipe, err := v2learning.DecodeFrozenRecipe(recipeRaw)
	if err != nil {
		return empty, ErrRegionInvalid
	}
	source, err := review.ParseWorkingImage(sourceRaw)
	if err != nil {
		return empty, ErrRegionInvalid
	}
	canvasSHA, err := canonical.Hash(source.Canvas)
	if err != nil || recipe.Source.AssetVersionID != req.VersionID || recipe.Source.Source != sourceRef || recipe.Source.CanvasHash != canvasSHA {
		return empty, ErrRegionInvalid
	}
	geometry, err := CompileRegionGeometry(source.Canvas.Width, source.Canvas.Height, req.Rect, req.MaxEdge)
	if err != nil {
		return empty, err
	}
	return CompiledRegionEvidence{geometry, source, recipe}, nil
}
