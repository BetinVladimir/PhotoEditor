package v2draft

import (
	"encoding/json"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// BaseCheckpoint preserves the exact source and StyleV2 reference of the
// first user recipe while removing all mutable edits. It is a baseline for
// future comparisons, never an implicit human approval.
func BaseCheckpoint(raw []byte, binding v2input.SessionVersionBinding) ([]byte, error) {
	if err := v2input.ValidateEditingRecipeV3(raw, binding); err != nil {
		return nil, err
	}
	var user struct {
		BaseStyle     v2input.Ref  `json:"base_style"`
		ProtectedMask *v2input.Ref `json:"protected_mask"`
	}
	if err := json.Unmarshal(raw, &user); err != nil {
		return nil, err
	}
	base := struct {
		SchemaVersion      int                           `json:"schema_version"`
		Source             v2input.SessionVersionBinding `json:"source"`
		BaseStyle          v2input.Ref                   `json:"base_style"`
		SessionCorrections *v2input.Ref                  `json:"session_corrections"`
		LearningVersionID  *string                       `json:"learning_version_id"`
		Overrides          []v2input.Ref                 `json:"overrides"`
		LayerStack         *v2input.Ref                  `json:"layer_stack"`
		ProtectedMask      *v2input.Ref                  `json:"protected_mask,omitempty"`
		RendererVersion    string                        `json:"renderer_version"`
		ParameterDelta     struct {
			EV              float64 `json:"ev"`
			LogR            float64 `json:"log_r"`
			LogG            float64 `json:"log_g"`
			LogB            float64 `json:"log_b"`
			SaturationDelta float64 `json:"saturation_delta"`
			RetouchDelta    float64 `json:"retouch_delta"`
		} `json:"parameter_delta"`
	}{SchemaVersion: 3, Source: binding, BaseStyle: user.BaseStyle,
		ProtectedMask: user.ProtectedMask,
		Overrides:     []v2input.Ref{}, RendererVersion: "v2.2"}
	body, err := canonical.Marshal(base)
	if err != nil {
		return nil, err
	}
	if err := v2input.ValidateEditingRecipeV3(body, binding); err != nil {
		return nil, err
	}
	return body, nil
}
