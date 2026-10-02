package v2learning

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type TrainingRecipeEvidence struct {
	Inputs         v2draft.ResolvedRecipeInputs
	LayerStackRef  *v2input.Ref
	LayerStackBody []byte
}

type CompiledTrainingRecipes struct {
	Base, Approved         FrozenRecipe
	BaseBody, ApprovedBody []byte
	BaseSHA, ApprovedSHA   string
}

// CompileTrainingRecipes uses the existing V2 scalar resolver and proves
// that layer activity was checked against the exact referenced LayerStack.
// The returned bytes still need an immutable training artifact publisher.
func CompileTrainingRecipes(documents TrainingEditingDocuments,
	baseEvidence, approvedEvidence TrainingRecipeEvidence) (
	CompiledTrainingRecipes, error) {
	var empty CompiledTrainingRecipes
	compile := func(body []byte, evidence TrainingRecipeEvidence) (
		[]byte, FrozenRecipe, string, error) {
		var recipe struct {
			LayerStack        *v2input.Ref `json:"layer_stack"`
			LearningVersionID *uuid.UUID   `json:"learning_version_id"`
		}
		if json.Unmarshal(body, &recipe) != nil ||
			(recipe.LayerStack == nil) != (evidence.LayerStackRef == nil) ||
			(recipe.LayerStack != nil && *recipe.LayerStack != *evidence.LayerStackRef) ||
			evidence.Inputs.Binding != documents.Binding ||
			evidence.Inputs.BaseStyle != documents.BaseStyle {
			return nil, FrozenRecipe{}, "", ErrInvalidTrainingDraft
		}
		if recipe.LearningVersionID == nil {
			if evidence.Inputs.LearningModelRef != (v2input.Ref{}) ||
				evidence.Inputs.LearningDescriptorRef != (v2input.Ref{}) ||
				evidence.Inputs.LearningAnalysisID != uuid.Nil ||
				evidence.Inputs.LearningDescriptorOrdinal != 0 {
				return nil, FrozenRecipe{}, "", ErrInvalidTrainingDraft
			}
		} else if evidence.Inputs.LearningID != *recipe.LearningVersionID ||
			!validManifestRef(evidence.Inputs.LearningModelRef) ||
			!validManifestRef(evidence.Inputs.LearningDescriptorRef) ||
			evidence.Inputs.LearningAnalysisID == uuid.Nil ||
			evidence.Inputs.LearningDescriptorOrdinal < 0 ||
			evidence.Inputs.LearningDescriptorOrdinal > 4999 {
			return nil, FrozenRecipe{}, "", ErrInvalidTrainingDraft
		}
		if recipe.LayerStack == nil {
			if len(evidence.LayerStackBody) != 0 {
				return nil, FrozenRecipe{}, "", ErrInvalidTrainingDraft
			}
		} else {
			canonicalBody, err := canonical.Bytes(evidence.LayerStackBody)
			sum := sha256.Sum256(evidence.LayerStackBody)
			if err != nil || !bytes.Equal(canonicalBody, evidence.LayerStackBody) ||
				hex.EncodeToString(sum[:]) != recipe.LayerStack.SHA256 {
				return nil, FrozenRecipe{}, "", ErrInvalidTrainingDraft
			}
			active, err := TrainingLayerActivity(evidence.LayerStackBody,
				documents.Binding)
			if err != nil || active {
				return nil, FrozenRecipe{}, "", ErrInvalidTrainingDraft
			}
		}
		frozenBody, err := v2draft.ResolveRecipe(body, evidence.Inputs)
		if err != nil {
			return nil, FrozenRecipe{}, "", err
		}
		frozen, err := DecodeFrozenRecipe(frozenBody)
		if err != nil {
			return nil, FrozenRecipe{}, "", err
		}
		sum := sha256.Sum256(frozenBody)
		return frozenBody, frozen, hex.EncodeToString(sum[:]), nil
	}
	baseBody, base, baseSHA, err := compile(documents.BaseBody, baseEvidence)
	if err != nil {
		return empty, err
	}
	approvedBody, approved, approvedSHA, err := compile(
		documents.ApprovedBody, approvedEvidence)
	if err != nil {
		return empty, err
	}
	if base.Source != approved.Source || base.BaseStyle != approved.BaseStyle ||
		base.ICCLockSHA != approved.ICCLockSHA ||
		base.ModelLockSHA != approved.ModelLockSHA {
		return empty, ErrInvalidTrainingDraft
	}
	return CompiledTrainingRecipes{Base: base, Approved: approved,
		BaseBody: baseBody, ApprovedBody: approvedBody,
		BaseSHA: baseSHA, ApprovedSHA: approvedSHA}, nil
}
