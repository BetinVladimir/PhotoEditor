package v2render

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// publishRecipeProtection preserves the full recipe's global protection as a
// standalone typed MaskRef. It is not a region, a selection, or a skin mask.
// Source is always the original recipe occurrence; caller refs cannot replace it.
func (s SQLAuthoredDetailMaterialization) publishRecipeProtection(ctx context.Context, who auth.Identity, source AuthoredInstructionSource, current []byte, proofs []CurrentAuthoredDetailTransform) (*v2input.Ref, []int, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	ref, err := authoredRecipeProtectionRef(source)
	if err != nil {
		return nil, nil, err
	}
	if ref == nil {
		return nil, []int{}, nil
	}
	original, err := s.Original.ReadJSON(ctx, who, source.Tenant, source.Project, source.Parent, source.Instruction.Ordinal, *ref)
	if err != nil {
		return nil, nil, err
	}
	documents := make([]AuthoredDetailTransformDocument, len(proofs))
	for i, p := range proofs {
		documents[i] = p.Document()
	}
	projected, indices, err := compileAuthoredMaskPaths([]json.RawMessage{original}, current, source.Source.Source, documents)
	if err != nil {
		return nil, nil, err
	}
	witnesses := make([]json.RawMessage, 0, len(indices))
	for _, i := range indices {
		w, e := proofs[i].Witness()
		if e != nil {
			return nil, nil, e
		}
		witnesses = append(witnesses, w)
	}
	wire, err := canonical.Marshal(witnesses)
	if err != nil {
		return nil, nil, err
	}
	published, err := s.Masks.Publish(ctx, source.Tenant, source.Parent, source.Instruction.Ordinal, projected[0], *ref, original, current, wire)
	if err != nil {
		return nil, nil, err
	}
	return &published, indices, nil
}

func authoredRecipeProtectionRef(source AuthoredInstructionSource) (*v2input.Ref, error) {
	if !authoredDetailCanonical([]byte(source.RecipeJCS)) || authoredDetailSHA([]byte(source.RecipeJCS)) != source.OriginalRecipe.SHA256 {
		return nil, v2input.ErrInvalid
	}
	var recipe struct {
		Recipe struct {
			Protected *v2input.Ref `json:"protected_mask"`
		} `json:"recipe"`
	}
	if json.Unmarshal([]byte(source.RecipeJCS), &recipe) != nil {
		return nil, v2input.ErrInvalid
	}
	if recipe.Recipe.Protected == nil {
		return nil, nil
	}
	ref := *recipe.Recipe.Protected
	if !validLayerArtifactRef(ref) {
		return nil, v2input.ErrInvalid
	}
	return &ref, nil
}
