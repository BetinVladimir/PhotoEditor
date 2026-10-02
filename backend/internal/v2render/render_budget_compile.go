package v2render

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"photoeditor/backend/internal/v2input"
)

// CompileRenderBudget derives dimensions and mask accounting from pinned
// documents. It does not authorize artifacts or qualify supplied provider peaks.
func CompileRenderBudget(binding v2input.SessionVersionBinding, recipeRef v2input.Ref, recipeRaw, sourceRaw, layerRaw []byte, tileSize int, alphaMode string, provider RenderProviderPeak) (RenderPlanBudget, error) {
	evidence, err := CompileRenderEvidence(binding, recipeRef, recipeRaw, sourceRaw)
	if err != nil {
		return RenderPlanBudget{}, err
	}
	var authored struct {
		LayerStack    *v2input.Ref `json:"layer_stack"`
		ProtectedMask *v2input.Ref `json:"protected_mask"`
	}
	if json.Unmarshal(evidence.Recipe.Recipe, &authored) != nil {
		return RenderPlanBudget{}, v2input.ErrInvalid
	}
	plan := Plan{Source: binding}
	if authored.LayerStack == nil {
		if len(layerRaw) != 0 {
			return RenderPlanBudget{}, v2input.ErrInvalid
		}
	} else {
		sum := sha256.Sum256(layerRaw)
		if hex.EncodeToString(sum[:]) != authored.LayerStack.SHA256 {
			return RenderPlanBudget{}, v2input.ErrInvalid
		}
		plan, err = Compile(layerRaw, binding)
		if err != nil {
			return RenderPlanBudget{}, err
		}
	}
	return plan.BudgetRender(int64(evidence.Source.Canvas.Width), int64(evidence.Source.Canvas.Height), authored.ProtectedMask, tileSize, alphaMode, provider)
}
