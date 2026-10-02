package v2render

import (
	"bytes"
	"photoeditor/backend/internal/v2input"
	"sort"
)

// RenderInputRoots returns the immutable roots for the scoped typed closure
// resolver. It deliberately does not flatten authored children into root grants:
// their occurrence/source/parent authority must be checked during traversal.
func RenderInputRoots(binding v2input.SessionVersionBinding, recipeRef v2input.Ref, recipeRaw, sourceRaw []byte) ([]v2input.Entry, error) {
	evidence, err := CompileRenderEvidence(binding, recipeRef, recipeRaw, sourceRaw)
	if err != nil {
		return nil, err
	}
	if evidence.RecipeInput.Ref.ArtifactID == evidence.SourceInput.Ref.ArtifactID {
		return nil, v2input.ErrInvalid
	}
	roots := []v2input.Entry{evidence.RecipeInput, evidence.SourceInput}
	sort.Slice(roots, func(i, j int) bool { return bytes.Compare(roots[i].Ref.ArtifactID[:], roots[j].Ref.ArtifactID[:]) < 0 })
	return roots, nil
}
