package v2learning

import (
	"bytes"
	"encoding/json"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type FrozenRecipe struct {
	SchemaVersion  int                           `json:"schema_version"`
	Source         v2input.SessionVersionBinding `json:"source"`
	BaseStyle      v2input.Ref                   `json:"base_style"`
	EffectiveDelta v2draft.CorrectionVector      `json:"effective_delta"`
	Renderer       string                        `json:"renderer_version"`
	ICCLockSHA     string                        `json:"icc_lock_sha"`
	ModelLockSHA   string                        `json:"model_lock_sha"`
}

// DecodeFrozenRecipe requires exact, canonical fields before any nested
// artifact may be added to the worker's input closure.
func DecodeFrozenRecipe(raw []byte) (FrozenRecipe, error) {
	var empty FrozenRecipe
	if len(raw) < 2 || len(raw) > 1048576 {
		return empty, ErrInvalidTrainingManifest
	}
	canonicalBody, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(canonicalBody, raw) {
		return empty, ErrInvalidTrainingManifest
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 7 {
		return empty, ErrInvalidTrainingManifest
	}
	for _, key := range []string{"schema_version", "source", "base_style",
		"effective_delta", "renderer_version", "icc_lock_sha", "model_lock_sha"} {
		if _, okay := fields[key]; !okay {
			return empty, ErrInvalidTrainingManifest
		}
	}
	if _, err := v2draft.DecodeCorrectionVector(fields["effective_delta"]); err != nil {
		return empty, ErrInvalidTrainingManifest
	}
	var recipe FrozenRecipe
	if json.Unmarshal(raw, &recipe) != nil || recipe.SchemaVersion != 1 ||
		recipe.Renderer != "v2.2" || !validManifestRef(recipe.BaseStyle) ||
		recipe.Source.AssetVersionID == [16]byte{} ||
		!validManifestRef(recipe.Source.Source) ||
		!validSHA(recipe.Source.CanvasHash) || !validSHA(recipe.ICCLockSHA) ||
		!validSHA(recipe.ModelLockSHA) {
		return empty, ErrInvalidTrainingManifest
	}
	encoded, err := canonical.Marshal(recipe)
	if err != nil || !bytes.Equal(encoded, raw) {
		return empty, ErrInvalidTrainingManifest
	}
	return recipe, nil
}

// ValidateFrozenRecipePair binds both human-approved and base recipes to the
// same source/style and the lock tuple used by the training manifest.
func ValidateFrozenRecipePair(baseRaw, approvedRaw []byte,
	caseRef TrainingRenderCase, manifest TrainingManifest) (v2input.Ref, error) {
	base, err := DecodeFrozenRecipe(baseRaw)
	if err != nil {
		return v2input.Ref{}, err
	}
	approved, err := DecodeFrozenRecipe(approvedRaw)
	if err != nil {
		return v2input.Ref{}, err
	}
	for _, recipe := range []FrozenRecipe{base, approved} {
		if recipe.Source != caseRef.Binding ||
			recipe.Renderer != manifest.RendererVersion ||
			recipe.ICCLockSHA != manifest.ICCLockSHA ||
			recipe.ModelLockSHA != manifest.ModelLockSHA {
			return v2input.Ref{}, ErrInvalidTrainingManifest
		}
	}
	if caseRef.Binding.Source != caseRef.WorkingImage ||
		base.BaseStyle != approved.BaseStyle {
		return v2input.Ref{}, ErrInvalidTrainingManifest
	}
 if !validTrainingBackgroundPin(caseRef.BackgroundAsset,manifest.ICCLockSHA,base.BaseStyle){return v2input.Ref{},ErrInvalidTrainingManifest}
	return base.BaseStyle, nil
}
