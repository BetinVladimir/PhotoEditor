package v2render

import (
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// CompileRenderExecutionProfile freezes resource and runtime inputs for task
// admission. Runtime SHA/provider peaks must come from qualified registry data;
// syntactically valid pins do not themselves establish qualification.
func CompileRenderExecutionProfile(binding v2input.SessionVersionBinding, recipeRef v2input.Ref, recipeRaw, sourceRaw, layerRaw []byte, backend, runtimeSHA string, tileSize int, alphaMode string, provider RenderProviderPeak) (json.RawMessage, error) {
	if backend != "cuda" && backend != "cpu" && backend != "mps" {
		return nil, v2input.ErrInvalid
	}
	if len(runtimeSHA) != 64 {
		return nil, v2input.ErrInvalid
	}
	for _, c := range runtimeSHA {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return nil, v2input.ErrInvalid
		}
	}
	evidence, err := CompileRenderEvidence(binding, recipeRef, recipeRaw, sourceRaw)
	if err != nil {
		return nil, err
	}
	budget, err := CompileRenderBudget(binding, recipeRef, recipeRaw, sourceRaw, layerRaw, tileSize, alphaMode, provider)
	if err != nil {
		return nil, err
	}
	return canonical.Marshal(struct {
		Backend    string                `json:"backend"`
		Renderer   string                `json:"renderer_version"`
		RuntimeSHA string                `json:"render_runtime_sha"`
		ModelSHA   string                `json:"model_lock_sha"`
		ICCSHA     string                `json:"icc_lock_sha"`
		TileSize   int                   `json:"tile_size"`
		AlphaMode  string                `json:"alpha_mode"`
		Resources  LayerResourceEstimate `json:"resource_estimate"`
	}{backend, "v2.2", runtimeSHA, evidence.Recipe.Scalars.ModelLockSHA, evidence.Recipe.Scalars.ICCLockSHA, tileSize, alphaMode, budget.Resources})
}
