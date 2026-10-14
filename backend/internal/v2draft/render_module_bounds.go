package v2draft

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/editor"
	"photoeditor/backend/internal/v2input"
)

// resolveRenderModuleDocuments retains the accepted base baseline as authority,
// but returns the scalar baseline after ordered whole-module replacement. It
// validates relevant scalar fields; full module/model schema validation remains
// mandatory in the worker and is not supplied by this metadata extraction.
func resolveRenderModuleDocuments(authored []byte, inputs ResolvedRecipeInputs,
	styleBody []byte, overrideBodies [][]byte, sourceSHA string) (StyleBaseline, json.RawMessage, error) {
	invalid := func() (StyleBaseline, json.RawMessage, error) { return StyleBaseline{}, nil, v2input.ErrInvalid }
	if !correctionDigest(sourceSHA) || len(overrideBodies) != len(inputs.Overrides) || v2input.ValidateEditingRecipeV3(authored, inputs.Binding) != nil {
		return invalid()
	}
	var recipe struct {
		BaseStyle v2input.Ref   `json:"base_style"`
		Overrides []v2input.Ref `json:"overrides"`
	}
	if json.Unmarshal(authored, &recipe) != nil || recipe.BaseStyle != inputs.BaseStyle || len(recipe.Overrides) != len(inputs.Overrides) {
		return invalid()
	}
	verify := func(raw []byte, ref v2input.Ref) bool {
		if len(raw) < 2 || len(raw) > 1048576 || !isDocumentRef(ref) {
			return false
		}
		sum := sha256.Sum256(raw)
		wire, err := canonical.Bytes(raw)
		return err == nil && bytes.Equal(wire, raw) && hex.EncodeToString(sum[:]) == ref.SHA256
	}
	if !verify(styleBody, inputs.BaseStyle) {
		return invalid()
	}
	base, err := DecodeStyleBaseline(styleBody)
	if err != nil || base != inputs.Style {
		return invalid()
	}
	var style map[string]json.RawMessage
	if json.Unmarshal(styleBody, &style) != nil || len(style["extensions"]) == 0 || string(style["extensions"]) == "null" {
		return invalid()
	}
	for i, raw := range overrideBodies {
		if recipe.Overrides[i] != inputs.Overrides[i].Ref || inputs.Overrides[i].Vector != (CorrectionVector{}) || !verify(raw, inputs.Overrides[i].Ref) || editor.ValidateOverride(raw, sourceSHA) != nil {
			return invalid()
		}
		var override struct {
			Modules map[string]json.RawMessage `json:"modules"`
		}
		if json.Unmarshal(raw, &override) != nil {
			return invalid()
		}
		for _, name := range []string{"global", "beauty", "extensions"} {
			if value, ok := override.Modules[name]; ok {
				style[name] = value
			}
		}
	}
	// Reject missing/null scalar fields explicitly: json.Unmarshal into numeric
	// fields alone would otherwise silently convert null to zero.
	var global struct {
		Gains      []json.RawMessage `json:"gains_rgb"`
		Saturation json.RawMessage   `json:"saturation"`
	}
	var beauty struct {
		Enabled   *bool           `json:"enabled"`
		Smoothing json.RawMessage `json:"smoothing"`
	}
	if json.Unmarshal(style["global"], &global) != nil || len(global.Gains) != 3 || !finiteJSONNumber(global.Saturation) || json.Unmarshal(style["beauty"], &beauty) != nil || beauty.Enabled == nil {
		return invalid()
	}
	for _, gain := range global.Gains {
		if !finiteJSONNumber(gain) {
			return invalid()
		}
	}
	if *beauty.Enabled {
		if !finiteJSONNumber(beauty.Smoothing) {
			return invalid()
		}
	} else {
		fields, ok := exactFields(style["beauty"], "enabled")
		if !ok || string(fields["enabled"]) != "false" {
			// Accepted legacy disabled styles may explicitly retain smoothing=0.
			fields, ok = exactFields(style["beauty"], "enabled", "smoothing")
			if !ok || string(fields["enabled"]) != "false" || !finiteJSONNumber(fields["smoothing"]) {
				return invalid()
			}
			var smoothing float64
			if json.Unmarshal(fields["smoothing"], &smoothing) != nil || smoothing != 0 {
				return invalid()
			}
		}
	}
	mergedRaw, err := canonical.Marshal(style)
	if err != nil {
		return invalid()
	}
	merged, err := DecodeStyleBaseline(mergedRaw)
	if err != nil {
		return invalid()
	}
	if _, err := backgroundPortableID(style["extensions"]); err != nil {
		return invalid()
	}
	return merged, style["extensions"], nil
}

// BuildFrozenRenderRecipeFromDocuments validates final scalar bounds against
// the merged module baseline. Base refs and authored provenance are unchanged.
func BuildFrozenRenderRecipeFromDocuments(authored []byte, inputs ResolvedRecipeInputs,
	layerBody, styleBody []byte, overrideBodies [][]byte, sourceSHA string) ([]byte, error) {
	merged, _, err := resolveRenderModuleDocuments(authored, inputs, styleBody, overrideBodies, sourceSHA)
	if err != nil {
		return nil, err
	}
	inputs.Style = merged
	return BuildFrozenRenderRecipe(authored, inputs, layerBody)
}
