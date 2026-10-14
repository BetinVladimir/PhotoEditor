package v2draft

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// FrozenRenderRecipe preserves authored composition and resolved scalar evidence.
// Rendering applies Scalars.EffectiveDelta once; recipe.parameter_delta and
// correction refs are retained as provenance and must not be applied again.
type FrozenRenderRecipe struct {
	SchemaVersion   int                    `json:"schema_version"`
	Recipe          json.RawMessage        `json:"recipe"`
	RecipeSHA256    string                 `json:"recipe_sha256"`
	Scalars         FrozenRecipe           `json:"scalars"`
	Learning        *FrozenRenderLearning  `json:"learning"`
	BackgroundAsset *FrozenBackgroundAsset `json:"background_asset,omitempty"`
}
type FrozenRenderLearning struct {
	VersionID  uuid.UUID   `json:"version_id"`
	Model      v2input.Ref `json:"model"`
	Descriptor v2input.Ref `json:"descriptor"`
	AnalysisID uuid.UUID   `json:"analysis_id"`
	Ordinal    int         `json:"ordinal"`
}

// BuildFrozenRenderRecipe preserves all authored fields; callers still prove
// current ACL, accepted source/style, immutable versions and the full closure.
// A referenced layer body must be independently verified before this compiler.
func BuildFrozenRenderRecipe(raw []byte, inputs ResolvedRecipeInputs, layerBody []byte) ([]byte, error) {
	return BuildFrozenRenderRecipeWithBackground(raw, inputs, layerBody, nil)
}

func BuildFrozenRenderRecipeWithBackground(raw []byte, inputs ResolvedRecipeInputs, layerBody, backgroundBody []byte) ([]byte, error) {
	canonicalRecipe, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(canonicalRecipe, raw) {
		return nil, v2input.ErrInvalid
	}
	scalarBody, err := ResolveRecipe(raw, inputs)
	if err != nil {
		return nil, err
	}
	var authored struct {
		LayerStack *v2input.Ref `json:"layer_stack"`
	}
	if json.Unmarshal(raw, &authored) != nil {
		return nil, v2input.ErrInvalid
	}
	if authored.LayerStack == nil {
		if len(layerBody) != 0 {
			return nil, v2input.ErrInvalid
		}
	} else {
		sum := sha256.Sum256(layerBody)
		if hex.EncodeToString(sum[:]) != authored.LayerStack.SHA256 {
			return nil, v2input.ErrInvalid
		}
		if _, err := ValidateLayerStack(layerBody, inputs.Binding); err != nil {
			return nil, err
		}
	}
	var scalars FrozenRecipe
	if json.Unmarshal(scalarBody, &scalars) != nil {
		return nil, v2input.ErrInvalid
	}
	var learning *FrozenRenderLearning
	if inputs.Learning != nil {
		if !isDocumentRef(inputs.LearningModelRef) || !isDocumentRef(inputs.LearningDescriptorRef) || inputs.LearningAnalysisID == uuid.Nil || inputs.LearningDescriptorOrdinal < 0 || inputs.LearningDescriptorOrdinal > 4999 {
			return nil, v2input.ErrInvalid
		}
		learning = &FrozenRenderLearning{inputs.LearningID, inputs.LearningModelRef, inputs.LearningDescriptorRef, inputs.LearningAnalysisID, inputs.LearningDescriptorOrdinal}
	} else if inputs.LearningModelRef != (v2input.Ref{}) || inputs.LearningDescriptorRef != (v2input.Ref{}) || inputs.LearningAnalysisID != uuid.Nil || inputs.LearningDescriptorOrdinal != 0 {
		return nil, v2input.ErrInvalid
	}
	var background *FrozenBackgroundAsset
	if len(backgroundBody) > 0 {
		pin, err := DecodeFrozenBackgroundAsset(backgroundBody)
		if err != nil || pin.Style != scalars.BaseStyle || pin.WorkICCSHA != scalars.ICCLockSHA {
			return nil, v2input.ErrInvalid
		}
		background = &pin
	}
	digest := sha256.Sum256(raw)
	out, err := canonical.Marshal(FrozenRenderRecipe{1, append(json.RawMessage(nil), raw...), hex.EncodeToString(digest[:]), scalars, learning, background})
	if err != nil || len(out) > 1048576 {
		return nil, v2input.ErrInvalid
	}
	return out, nil
}

// DecodeFrozenRenderRecipe validates the full immutable wire before dependency
// traversal. Recomputed scalar evidence and current authorization remain required.
func DecodeFrozenRenderRecipe(raw []byte) (FrozenRenderRecipe, error) {
	var frozen FrozenRenderRecipe
	invalid := func() (FrozenRenderRecipe, error) { return FrozenRenderRecipe{}, v2input.ErrInvalid }
	if len(raw) > 1048576 || json.Unmarshal(raw, &frozen) != nil {
		return invalid()
	}
	wire, err := canonical.Marshal(frozen)
	if err != nil || !bytes.Equal(wire, raw) || frozen.SchemaVersion != 1 {
		return invalid()
	}
	scalars := frozen.Scalars
	if scalars.SchemaVersion != 1 || scalars.RendererVersion != "v2.2" || !scalars.EffectiveDelta.valid() || !correctionDigest(scalars.ICCLockSHA) || !correctionDigest(scalars.ModelLockSHA) || !isDocumentRef(scalars.BaseStyle) || v2input.ValidateEditingRecipeV3(frozen.Recipe, scalars.Source) != nil {
		return invalid()
	}
	digest := sha256.Sum256(frozen.Recipe)
	if frozen.RecipeSHA256 != hex.EncodeToString(digest[:]) {
		return invalid()
	}
	var recipe struct {
		BaseStyle         v2input.Ref `json:"base_style"`
		LearningVersionID *uuid.UUID  `json:"learning_version_id"`
	}
	if json.Unmarshal(frozen.Recipe, &recipe) != nil || recipe.BaseStyle != scalars.BaseStyle || (recipe.LearningVersionID == nil) != (frozen.Learning == nil) {
		return invalid()
	}
	if learning := frozen.Learning; learning != nil {
		if learning.VersionID == uuid.Nil || learning.VersionID != *recipe.LearningVersionID || learning.AnalysisID == uuid.Nil || learning.Ordinal < 0 || learning.Ordinal > 4999 || !isDocumentRef(learning.Model) || !isDocumentRef(learning.Descriptor) {
			return invalid()
		}
	}
	if frozen.BackgroundAsset != nil {
		raw, err := canonical.Marshal(frozen.BackgroundAsset)
		if err != nil {
			return invalid()
		}
		pin, err := DecodeFrozenBackgroundAsset(raw)
		if err != nil || pin.Style != scalars.BaseStyle || pin.WorkICCSHA != scalars.ICCLockSHA {
			return invalid()
		}
	}
	return frozen, nil
}
