package v2draft

import (
	"encoding/json"
	"photoeditor/backend/internal/v2input"
	"strconv"
)

func renderRecipeRefs(raw []byte, path string, parent v2input.Ref) ([]LayerRef, error) {
	frozen, err := DecodeFrozenRenderRecipe(raw)
	if err != nil || !isDocumentRef(parent) {
		return nil, v2input.ErrInvalid
	}
	var recipe struct {
		Session    *v2input.Ref  `json:"session_corrections"`
		Overrides  []v2input.Ref `json:"overrides"`
		Layers     *v2input.Ref  `json:"layer_stack"`
		Protection *v2input.Ref  `json:"protected_mask"`
	}
	if json.Unmarshal(frozen.Recipe, &recipe) != nil {
		return nil, v2input.ErrInvalid
	}
	children := []LayerRef{}
	add := func(suffix, contract string, ref v2input.Ref) {
		children = append(children, LayerRef{Path: path + suffix, Contract: contract, Ref: ref, ParentRef: parent})
	}
	add(".recipe.source.source", "WorkingImage", frozen.Scalars.Source.Source)
	children[0].CanvasHash = frozen.Scalars.Source.CanvasHash
	add(".recipe.base_style", "StyleV2", frozen.Scalars.BaseStyle)
	if recipe.Session != nil {
		add(".recipe.session_corrections", "CorrectionPlan", *recipe.Session)
	}
	for i, ref := range recipe.Overrides {
		add(".recipe.overrides["+strconv.Itoa(i)+"]", "Override", ref)
	}
	if recipe.Layers != nil {
		add(".recipe.layer_stack", "LayerStack", *recipe.Layers)
	}
	if recipe.Protection != nil {
		add(".recipe.protected_mask", "MaskRef", *recipe.Protection)
	}
	if frozen.Learning != nil {
		add(".learning.model", "CandidateProfile", frozen.Learning.Model)
		modelLearning := *frozen.Learning
		children[len(children)-1].ExpectedLearning = &modelLearning
		modelBinding := frozen.Scalars.Source
		children[len(children)-1].ExpectedBinding = &modelBinding
		add(".learning.descriptor", "DescriptorShard", frozen.Learning.Descriptor)
		descriptorLearning := *frozen.Learning
		children[len(children)-1].ExpectedLearning = &descriptorLearning
		children[len(children)-1].ExpectedModelLockSHA = frozen.Scalars.ModelLockSHA
		descriptorBinding := frozen.Scalars.Source
		children[len(children)-1].ExpectedBinding = &descriptorBinding
	}
	return children, nil
}
