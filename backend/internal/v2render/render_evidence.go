package v2render

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// CompiledRenderEvidence retains full authored composition. Callers must still
// prove scoped catalog authority, immutable object versions and dependency closure.
type CompiledRenderEvidence struct {
	Recipe      v2draft.FrozenRenderRecipe
	Source      review.WorkingImage
	RecipeInput v2input.Entry
	SourceInput v2input.Entry
}

// CompileRenderEvidence validates immutable full-render documents against an
// independently admitted version binding. It does not resolve scalar evidence,
// qualify providers, perform IO or grant permission to a dependency.
func CompileRenderEvidence(binding v2input.SessionVersionBinding, recipeRef v2input.Ref, recipeRaw, sourceRaw []byte) (CompiledRenderEvidence, error) {
	invalid := func() (CompiledRenderEvidence, error) { return CompiledRenderEvidence{}, v2input.ErrInvalid }
	if binding.AssetVersionID == uuid.Nil || binding.Source.ArtifactID == uuid.Nil || recipeRef.ArtifactID == uuid.Nil || len(recipeRaw) == 0 || len(recipeRaw) > 1048576 || len(sourceRaw) == 0 || len(sourceRaw) > 1048576 {
		return invalid()
	}
	digest := func(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
	if digest(recipeRaw) != recipeRef.SHA256 || digest(sourceRaw) != binding.Source.SHA256 {
		return invalid()
	}
	recipe, err := v2draft.DecodeFrozenRenderRecipe(recipeRaw)
	if err != nil || recipe.Scalars.Source != binding {
		return invalid()
	}
	source, err := review.ParseWorkingImage(sourceRaw)
	if err != nil {
		return invalid()
	}
	wire, err := canonical.Bytes(sourceRaw)
	if err != nil || !bytes.Equal(wire, sourceRaw) {
		return invalid()
	}
	canvasSHA, err := canonical.Hash(source.Canvas)
	if err != nil || canvasSHA != binding.CanvasHash {
		return invalid()
	}
	return CompiledRenderEvidence{recipe, source, v2input.Entry{Ref: recipeRef, Contract: "FrozenRenderRecipe"}, v2input.Entry{Ref: binding.Source, Contract: "WorkingImage"}}, nil
}
