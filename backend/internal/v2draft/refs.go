package v2draft

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"photoeditor/backend/internal/v2input"
)

// DocumentRef is the wire shape accepted by register_v2_document_refs.
// Purpose is a typed JSON path hash; ordinal follows stable depth-first order.
type DocumentRef struct {
	ArtifactID string `json:"artifact_id"`
	SHA256     string `json:"sha256"`
	Purpose    string `json:"purpose"`
	Ordinal    int    `json:"ordinal"`
}

// DirectRefs extracts the root references from a validated recipe. Callers
// must walk and verify the transitive closure before publishing the document.
func DirectRefs(raw []byte, binding v2input.SessionVersionBinding) ([]DocumentRef, error) {
	roots, err := recipeRoots(raw, binding)
	if err != nil {
		return nil, err
	}
	refs := make([]DocumentRef, 0, len(roots))
	for _, root := range roots {
		refs = append(refs, documentRef(root, len(refs)))
	}
	return refs, nil
}

func documentRef(root LayerRef, ordinal int) DocumentRef {
	purpose := sha256.Sum256([]byte(root.Path + ":" + root.Contract))
	return DocumentRef{ArtifactID: root.Ref.ArtifactID.String(),
		SHA256: root.Ref.SHA256, Purpose: hex.EncodeToString(purpose[:]), Ordinal: ordinal}
}

func recipeRoots(raw []byte, binding v2input.SessionVersionBinding) ([]LayerRef, error) {
	if err := v2input.ValidateEditingRecipeV3(raw, binding); err != nil {
		return nil, err
	}
	var recipe struct {
		BaseStyle          v2input.Ref   `json:"base_style"`
		SessionCorrections *v2input.Ref  `json:"session_corrections"`
		Overrides          []v2input.Ref `json:"overrides"`
		LayerStack         *v2input.Ref  `json:"layer_stack"`
		ProtectedMask      *v2input.Ref  `json:"protected_mask"`
	}
	if err := json.Unmarshal(raw, &recipe); err != nil {
		return nil, err
	}
	refs := make([]LayerRef, 0, 4+len(recipe.Overrides))
	add := func(path, contract string, ref v2input.Ref) {
		root := LayerRef{Path: path, Contract: contract, Ref: ref}
		if contract == "WorkingImage" {
			root.CanvasHash = binding.CanvasHash
		}
		refs = append(refs, root)
	}
	add("EditingRecipeV3.source.source", "WorkingImage", binding.Source)
	add("EditingRecipeV3.base_style", "StyleV2", recipe.BaseStyle)
	if recipe.SessionCorrections != nil {
		add("EditingRecipeV3.session_corrections", "CorrectionPlan", *recipe.SessionCorrections)
	}
	for i, ref := range recipe.Overrides {
		add(fmt.Sprintf("EditingRecipeV3.overrides[%d]", i), "Override", ref)
	}
	if recipe.LayerStack != nil {
		add("EditingRecipeV3.layer_stack", "LayerStack", *recipe.LayerStack)
	}
	if recipe.ProtectedMask != nil {
		add("EditingRecipeV3.protected_mask", "MaskRef", *recipe.ProtectedMask)
	}
	return refs, nil
}
