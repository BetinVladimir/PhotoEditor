package v2learning

import (
	"encoding/json"

	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// TrainingLayerActivity parses the verified LayerStack body. Disabled and
// zero-opacity layers are retained in the editor but have no learned scalar
// effect; an active layer makes the event nonparametric.
func TrainingLayerActivity(raw []byte,
	binding v2input.SessionVersionBinding) (bool, error) {
	if _, err := v2draft.ValidateLayerStack(raw, binding); err != nil {
		return false, ErrInvalidTrainingDraft
	}
	var stack struct {
		Layers []struct {
			Enabled bool    `json:"enabled"`
			Opacity float64 `json:"opacity"`
		} `json:"layers"`
	}
	if json.Unmarshal(raw, &stack) != nil {
		return false, ErrInvalidTrainingDraft
	}
	for _, layer := range stack.Layers {
		if layer.Enabled && layer.Opacity > 0 {
			return true, nil
		}
	}
	return false, nil
}
