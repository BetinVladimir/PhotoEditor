package v2render

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// LayerBudget describes one active layer's reachable mask graph. It does not
// include inactive layers or unrelated masks; estimates remain conservative for
// parent RGB/alpha/generated coverage, which resolve at execution time.
type LayerBudget struct {
	Masks     []MaskNode
	MaskRefs  int
	Resources LayerResourceEstimate
}

func (p Plan) BudgetLayer(id uuid.UUID, width, height int64, protected *v2input.Ref, tileSize int) (LayerBudget, error) {
	var selected *PlannedLayer
	for i := range p.Layers {
		if p.Layers[i].Layer.ID == id {
			if selected != nil {
				return LayerBudget{}, v2input.ErrInvalid
			}
			selected = &p.Layers[i]
		}
	}
	if id == uuid.Nil || selected == nil || !selected.Layer.Active() {
		return LayerBudget{}, v2input.ErrInvalid
	}
	sub := p
	sub.Layers = []PlannedLayer{*selected}
	masks, err := sub.ActiveMasks()
	if err != nil || len(masks) < 1 || len(masks) > 256 {
		return LayerBudget{}, v2input.ErrInvalid
	}
	refs := map[v2input.Ref]bool{}

	for i, node := range masks {
		switch node.Operation {
		case "ref":
			if node.Ref == nil || !validLayerArtifactRef(*node.Ref) || node.Left != "" || node.Right != "" || node.Operand != "" {
				return LayerBudget{}, v2input.ErrInvalid
			}
			ref := *node.Ref
			refs[ref] = true
			masks[i].Ref = &ref
		case "invert":
			if node.Ref != nil || node.Operand == "" || node.Left != "" || node.Right != "" {
				return LayerBudget{}, v2input.ErrInvalid
			}
		case "union", "intersect", "subtract":
			if node.Ref != nil || node.Left == "" || node.Right == "" || node.Operand != "" {
				return LayerBudget{}, v2input.ErrInvalid
			}
		default:
			return LayerBudget{}, v2input.ErrInvalid
		}
	}
	if protected != nil {
		if !validLayerArtifactRef(*protected) {
			return LayerBudget{}, v2input.ErrInvalid
		}
		refs[*protected] = true
	}
	resources, err := LayerResources(width, height, len(refs), len(masks), tileSize)
	if err != nil {
		return LayerBudget{}, err
	}
	return LayerBudget{Masks: masks, MaskRefs: len(refs), Resources: resources}, nil
}
