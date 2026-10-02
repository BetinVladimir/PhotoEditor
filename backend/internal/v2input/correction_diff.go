package v2input

import (
	"bytes"
	"encoding/json"
	"reflect"
)

// DiffCorrectionParameters derives the six trainable paths from two immutable
// recipes. Changes to referenced styles, plans, models or layers need their
// own verified semantic diff and must never be disguised as scalar edits.
func DiffCorrectionParameters(base, approved []byte, binding SessionVersionBinding) ([]string, error) {
	if ValidateEditingRecipeV3(base, binding) != nil ||
		ValidateEditingRecipeV3(approved, binding) != nil {
		return nil, ErrInvalid
	}
	var before, after map[string]json.RawMessage
	if json.Unmarshal(base, &before) != nil || json.Unmarshal(approved, &after) != nil {
		return nil, ErrInvalid
	}
	for _, key := range []string{"schema_version", "source", "base_style",
		"session_corrections", "learning_version_id", "overrides",
		"layer_stack", "renderer_version"} {
		var left, right any
		if json.Unmarshal(before[key], &left) != nil ||
			json.Unmarshal(after[key], &right) != nil || !reflect.DeepEqual(left, right) {
			return nil, ErrInvalid
		}
	}
	if !bytes.Equal(before["protected_mask"], after["protected_mask"]) {
		return nil, ErrInvalid
	}
	var left, right map[string]json.RawMessage
	if json.Unmarshal(before["parameter_delta"], &left) != nil ||
		json.Unmarshal(after["parameter_delta"], &right) != nil {
		return nil, ErrInvalid
	}
	paths := make([]string, 0, 6)
	for _, key := range []string{"ev", "log_r", "log_g", "log_b",
		"saturation_delta", "retouch_delta"} {
		var x, y float64
		if json.Unmarshal(left[key], &x) != nil || json.Unmarshal(right[key], &y) != nil {
			return nil, ErrInvalid
		}
		if x != y {
			paths = append(paths, "/"+key)
		}
	}
	if len(paths) == 0 || bytes.Equal(base, approved) {
		return nil, ErrInvalid
	}
	return paths, nil
}
