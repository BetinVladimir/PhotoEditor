package v2draft

import (
	"encoding/json"
	"math"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/editor"
	"photoeditor/backend/internal/styleplan"
	"photoeditor/backend/internal/stylevalidation"
	"photoeditor/backend/internal/v2input"
	"sort"
)

// EditorEffectCompilation is detached compiler output. Admission must bind it
// to the frozen recipe/source and live editor authority before scheduling it.
// It does not replace the authored recipe, masks, layers or its base-style pin.
type EditorEffectCompilation struct {
	Style    json.RawMessage   `json:"style"`
	Override json.RawMessage   `json:"override"`
	Stages   []styleplan.Stage `json:"stages"`
}

// CompileEditorEffects materializes the complete ordered Style module stream
// and scalar correction exactly once for the existing Apply stage materializer.
// Every input document has already passed the same immutable pin/source proof
// used by full-render freezing. No module is disabled to obtain coverage.
func CompileEditorEffects(authored []byte, inputs ResolvedRecipeInputs, styleBody []byte, overrideBodies [][]byte, sourceSHA string) (EditorEffectCompilation, error) {
	invalid := func() (EditorEffectCompilation, error) { return EditorEffectCompilation{}, v2input.ErrInvalid }
	baseline, _, err := resolveRenderModuleDocuments(authored, inputs, styleBody, overrideBodies, sourceSHA)
	if err != nil {
		return invalid()
	}
	inputs.Style = baseline
	scalarRaw, err := ResolveRecipe(authored, inputs)
	if err != nil {
		return invalid()
	}
	var scalar FrozenRecipe
	if json.Unmarshal(scalarRaw, &scalar) != nil {
		return invalid()
	}
	var style map[string]json.RawMessage
	if json.Unmarshal(styleBody, &style) != nil {
		return invalid()
	}
	merged := map[string]json.RawMessage{}
	masks := map[string]json.RawMessage{}
	polygons := map[int]json.RawMessage{}
	decisions := map[string]bool{}
	for _, body := range overrideBodies {
		var fields map[string]json.RawMessage
		if json.Unmarshal(body, &fields) != nil {
			return invalid()
		}
		var modules map[string]json.RawMessage
		if json.Unmarshal(fields["modules"], &modules) != nil {
			return invalid()
		}
		for _, name := range []string{"strength", "global", "local", "beauty", "defects", "geometry", "extensions"} {
			if raw, ok := modules[name]; ok && string(raw) != "null" {
				style[name] = append(json.RawMessage(nil), raw...)
			}
		}
		var bindings []json.RawMessage
		if raw, ok := fields["mask_refs"]; ok {
			if json.Unmarshal(raw, &bindings) != nil {
				return invalid()
			}
			for _, raw := range bindings {
				var binding struct {
					ID string `json:"binding_id"`
				}
				if json.Unmarshal(raw, &binding) != nil || binding.ID == "" {
					return invalid()
				}
				masks[binding.ID] = raw
			}
		}
		var protected []json.RawMessage
		if raw, ok := fields["protected_polygons"]; ok {
			if json.Unmarshal(raw, &protected) != nil {
				return invalid()
			}
			for _, raw := range protected {
				var polygon struct {
					Sequence int `json:"sequence"`
				}
				if json.Unmarshal(raw, &polygon) != nil {
					return invalid()
				}
				polygons[polygon.Sequence] = raw
			}
		}
		for _, name := range []string{"accepted_candidate_ids", "rejected_candidate_ids"} {
			var ids []string
			if raw, ok := fields[name]; ok {
				if json.Unmarshal(raw, &ids) != nil {
					return invalid()
				}
				for _, id := range ids {
					decisions[id] = name == "accepted_candidate_ids"
				}
			}
		}
		for _, name := range []string{"object_id", "manual_landmarks"} {
			if raw, ok := fields[name]; ok && string(raw) != "null" {
				merged[name] = raw
			}
		}
	}
	var global map[string]json.RawMessage
	var gains []float64
	var saturation float64
	if json.Unmarshal(style["global"], &global) != nil || json.Unmarshal(global["gains_rgb"], &gains) != nil || len(gains) != 3 || json.Unmarshal(global["saturation"], &saturation) != nil {
		return invalid()
	}
	delta := scalar.EffectiveDelta
	logs := []float64{delta.LogR, delta.LogG, delta.LogB}
	for i := range gains {
		gains[i] *= math.Pow(2, delta.EV) * math.Exp(logs[i])
		if !inRange(gains[i], .25, 4) {
			return invalid()
		}
	}
	saturation += delta.SaturationDelta
	if !inRange(saturation, 0, 2) {
		return invalid()
	}
	global["gains_rgb"], _ = canonical.Marshal(gains)
	global["saturation"], _ = canonical.Marshal(saturation)
	style["global"], _ = canonical.Marshal(global)
	var beauty map[string]json.RawMessage
	var enabled bool
	var smoothing float64
	if json.Unmarshal(style["beauty"], &beauty) != nil || json.Unmarshal(beauty["enabled"], &enabled) != nil {
		return invalid()
	}
	if enabled && json.Unmarshal(beauty["smoothing"], &smoothing) != nil {
		return invalid()
	}
	smoothing += delta.RetouchDelta
	if !inRange(smoothing, 0, 1) {
		return invalid()
	}
	if enabled {
		beauty["smoothing"], _ = canonical.Marshal(smoothing)
	} else if smoothing > 0 {
		raw, _ := canonical.Marshal(map[string]any{"enabled": true, "algorithm": "beautyfilter_v1", "smoothing": smoothing, "tone_evenness": 0, "texture_keep": 1, "skin_mask": "face68_conservative_v1", "protect_features": true})
		json.Unmarshal(raw, &beauty)
	}
	style["beauty"], _ = canonical.Marshal(beauty)
	// V2 replaces the whole extensions document; Apply overlays its slots.
	// Explicit clears preserve V2 semantics when the accepted base had effects.
	var extensions map[string]json.RawMessage
	if json.Unmarshal(style["extensions"], &extensions) != nil {
		return invalid()
	}
	if extensions == nil {
		extensions = map[string]json.RawMessage{}
	}
	for _, slot := range []string{"culling", "series", "composition", "portrait_detail", "masking", "restoration", "background", "inpainting", "generation"} {
		if _, exists := extensions[slot]; !exists {
			extensions[slot] = json.RawMessage(`null`)
		}
	}
	if _, exists := extensions["region_adjustments"]; !exists {
		extensions["region_adjustments"] = json.RawMessage(`[]`)
	}
	style["extensions"], _ = canonical.Marshal(extensions)
	modules := map[string]json.RawMessage{}
	for _, name := range []string{"strength", "global", "local", "beauty", "defects", "geometry", "extensions"} {
		raw, ok := style[name]
		if !ok {
			return invalid()
		}
		valid := false
		switch name {
		case "strength":
			var strength *float64
			valid = json.Unmarshal(raw, &strength) == nil && strength != nil && *strength >= 0 && *strength <= 1
		case "global":
			valid = stylevalidation.Color(raw)
		case "extensions":
			valid = stylevalidation.Extensions(raw)
		default:
			valid = stylevalidation.Module(name, raw)
		}
		if !valid {
			return invalid()
		}
		modules[name] = raw
	}
	merged["schema_version"] = json.RawMessage(`1`)
	merged["revision"] = json.RawMessage(`1`)
	merged["input_sha256"], _ = canonical.Marshal(sourceSHA)
	merged["modules"], _ = canonical.Marshal(modules)
	keys := make([]string, 0, len(masks))
	for id := range masks {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	bindings := make([]json.RawMessage, 0, len(keys))
	for _, id := range keys {
		bindings = append(bindings, masks[id])
	}
	if len(bindings) > 0 {
		merged["mask_refs"], _ = canonical.Marshal(bindings)
	}
	sequences := make([]int, 0, len(polygons))
	for sequence := range polygons {
		sequences = append(sequences, sequence)
	}
	sort.Ints(sequences)
	protected := make([]json.RawMessage, 0, len(sequences))
	for _, sequence := range sequences {
		protected = append(protected, polygons[sequence])
	}
	if len(protected) > 0 {
		merged["protected_polygons"], _ = canonical.Marshal(protected)
	}
	accepted, rejected := []string{}, []string{}
	for id, accept := range decisions {
		if accept {
			accepted = append(accepted, id)
		} else {
			rejected = append(rejected, id)
		}
	}
	sort.Strings(accepted)
	sort.Strings(rejected)
	if len(accepted) > 0 {
		merged["accepted_candidate_ids"], _ = canonical.Marshal(accepted)
	}
	if len(rejected) > 0 {
		merged["rejected_candidate_ids"], _ = canonical.Marshal(rejected)
	}
	override, err := canonical.Marshal(merged)
	if err != nil || editor.ValidateOverride(override, sourceSHA) != nil {
		return invalid()
	}
	effective, err := canonical.Marshal(style)
	if err != nil {
		return invalid()
	}
	stages, err := styleplan.Compile(effective, 1)
	if err != nil {
		return invalid()
	}
	return EditorEffectCompilation{effective, override, stages}, nil
}
