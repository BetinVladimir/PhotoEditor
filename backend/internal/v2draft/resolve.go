package v2draft

import (
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// ResolvedRecipeInputs are verified, pinned dependencies of one immutable
// EditingRecipeV3. The caller must read and verify each referenced artifact
// and resolve a learning prediction against the frozen source descriptor.
type ResolvedRecipeInputs struct {
	Binding      v2input.SessionVersionBinding
	BaseStyle    v2input.Ref
	Style        StyleBaseline
	Session      *ResolvedCorrection
	LearningID   uuid.UUID
	Learning     *CorrectionVector
	LearningModelRef v2input.Ref
	LearningDescriptorRef v2input.Ref
	LearningAnalysisID uuid.UUID
	LearningDescriptorOrdinal int
	Overrides    []ResolvedCorrection
	ICCLockSHA   string
	ModelLockSHA string
}

type ResolvedCorrection struct {
	Ref    v2input.Ref
	Vector CorrectionVector
}

// ResolveRecipe combines the six scalar coordinates in contract order and
// refuses missing or extra resolved dependencies. It performs the final
// StyleV2 bounds check without silently clamping an authored edit.
func ResolveRecipe(raw []byte, inputs ResolvedRecipeInputs) ([]byte, error) {
	if v2input.ValidateEditingRecipeV3(raw, inputs.Binding) != nil ||
		!isDocumentRef(inputs.BaseStyle) {
		return nil, v2input.ErrInvalid
	}
	var recipe struct {
		BaseStyle          v2input.Ref      `json:"base_style"`
		SessionCorrections *v2input.Ref     `json:"session_corrections"`
		LearningVersionID  *uuid.UUID       `json:"learning_version_id"`
		Overrides          []v2input.Ref    `json:"overrides"`
		ParameterDelta     CorrectionVector `json:"parameter_delta"`
	}
	if json.Unmarshal(raw, &recipe) != nil || recipe.BaseStyle != inputs.BaseStyle ||
		(recipe.SessionCorrections == nil) != (inputs.Session == nil) ||
		(recipe.LearningVersionID == nil) != (inputs.Learning == nil) ||
		len(recipe.Overrides) != len(inputs.Overrides) {
		return nil, v2input.ErrInvalid
	}
	if recipe.SessionCorrections != nil && *recipe.SessionCorrections != inputs.Session.Ref {
		return nil, v2input.ErrInvalid
	}
	if recipe.LearningVersionID == nil && inputs.LearningID != uuid.Nil ||
		recipe.LearningVersionID != nil && *recipe.LearningVersionID != inputs.LearningID {
		return nil, v2input.ErrInvalid
	}
	for i, item := range recipe.Overrides {
		if item != inputs.Overrides[i].Ref {
			return nil, v2input.ErrInvalid
		}
	}
	parts := make([]CorrectionVector, 0, len(inputs.Overrides)+3)
	if inputs.Session != nil {
		parts = append(parts, inputs.Session.Vector)
	}
	if inputs.Learning != nil {
		parts = append(parts, *inputs.Learning)
	}
	for _, item := range inputs.Overrides {
		parts = append(parts, item.Vector)
	}
	parts = append(parts, recipe.ParameterDelta)
	delta, err := SumCorrectionVectors(parts...)
	if err != nil {
		return nil, err
	}
	return BuildFrozenRecipe(inputs.Binding, inputs.BaseStyle, inputs.Style,
		delta, inputs.ICCLockSHA, inputs.ModelLockSHA)
}
