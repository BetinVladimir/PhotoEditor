package v2input

import (
	"encoding/json"
	"math"
	"photoeditor/backend/internal/canonical"
)

// Versioned runtime accounting only: no model or image-algorithm changes.
// Legacy compilers retain exact profile bytes; qualified preparation opts in.
func (f FrozenSemanticFaceSkinTask) WithInputIndexScratch() (FrozenSemanticFaceSkinTask, error) {
	var zero FrozenSemanticFaceSkinTask
	var profile map[string]any
	if json.Unmarshal([]byte(f.profile), &profile) != nil || profile["backend"] != "cpu" || profile["renderer_version"] != "v2.2" {
		return zero, ErrInvalid
	}
	if _, exists := profile["input_index_scratch_bytes"]; exists {
		return zero, ErrConflict
	}
	resources, ok := profile["resource_estimate"].(map[string]any)
	if !ok {
		return zero, ErrInvalid
	}
	scratch, ok := resources["scratch_bytes"].(float64)
	if !ok || scratch < 0 || math.Trunc(scratch) != scratch || scratch > 9007199254740991-(16<<20) {
		return zero, ErrInvalid
	}
	resources["scratch_bytes"] = scratch + (16 << 20)
	profile["input_index_scratch_bytes"] = 16 << 20
	raw, err := canonical.Marshal(profile)
	if err != nil {
		return zero, err
	}
	f.profile = string(raw)
	return f, nil
}
