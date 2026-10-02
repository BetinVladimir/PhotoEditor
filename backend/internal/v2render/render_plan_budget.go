package v2render

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

type RenderPlanBudget struct {
	MaskRefs  int
	MaskNodes int
	Resources LayerResourceEstimate
}

// BudgetRender mirrors worker per-layer mask compilation: shared refs count
// once, but each active layer's reachable graph contributes temporary nodes.
// Provider peaks must be supplied from qualified runtime evidence.
func (p Plan) BudgetRender(width, height int64, protected *v2input.Ref, tileSize int, alphaMode string, provider RenderProviderPeak) (RenderPlanBudget, error) {
	refs := map[uuid.UUID]string{}
	add := func(ref v2input.Ref) error {
		if !validLayerArtifactRef(ref) {
			return v2input.ErrInvalid
		}
		if previous, ok := refs[ref.ArtifactID]; ok && previous != ref.SHA256 {
			return v2input.ErrInvalid
		}
		refs[ref.ArtifactID] = ref.SHA256
		return nil
	}
	nodes := 0
	for _, layer := range p.Layers {
		if !layer.Layer.Active() {
			continue
		}
		budget, err := p.BudgetLayer(layer.Layer.ID, width, height, nil, tileSize)
		if err != nil {
			return RenderPlanBudget{}, err
		}
		nodes += len(budget.Masks)
		for _, node := range budget.Masks {
			if node.Ref != nil {
				if err = add(*node.Ref); err != nil {
					return RenderPlanBudget{}, err
				}
			}
		}
	}
	if protected != nil {
		if err := add(*protected); err != nil {
			return RenderPlanBudget{}, err
		}
		nodes++
	}
	resources, err := RenderResources(width, height, len(refs), nodes, tileSize, alphaMode, provider)
	if err != nil {
		return RenderPlanBudget{}, err
	}
	return RenderPlanBudget{MaskRefs: len(refs), MaskNodes: nodes, Resources: resources}, nil
}
