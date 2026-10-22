package v2render

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// AuthoredCurrentDetailMaskBinding names standalone published MaskRef documents.
// This structural mapping is not an adopted-transform capability. The physical
// reader and SQL publication gate independently authenticate those documents.
type AuthoredCurrentDetailMaskBinding struct {
	Source  v2input.Ref `json:"source"`
	Current v2input.Ref `json:"current"`
}

// CompileAuthoredCurrentDetailLayers projects only the selected original detail
// occurrence. Every original setting survives; only Params and mask references
// change. It does not read objects, publish documents or grant admission.
func CompileAuthoredCurrentDetailLayers(ctx context.Context, source AuthoredInstructionSource, params v2input.Ref, bindings []AuthoredCurrentDetailMaskBinding) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if source.Tenant == uuid.Nil || source.Parent == uuid.Nil || source.Instruction.Kind != "segment" || source.Instruction.GeometryLayer != nil || len(source.Instruction.Layers) != 1 || source.Instruction.Layers[0] == uuid.Nil || !authoredBindingValid(source.Source) || !validLayerArtifactRef(params) || len(bindings) > 256 {
		return nil, v2input.ErrInvalid
	}
	raw := []byte(source.LayerStackJCS)
	if !authoredDetailCanonical(raw) {
		return nil, v2input.ErrInvalid
	}
	var original struct {
		Source v2input.SessionVersionBinding `json:"source"`
		Layers []json.RawMessage             `json:"layers"`
	}
	if json.Unmarshal(raw, &original) != nil || original.Source.AssetVersionID != source.Source.AssetVersionID {
		return nil, v2input.ErrInvalid
	}
	if _, err := v2draft.ValidateLayerStack(raw, original.Source); err != nil {
		return nil, err
	}
	var selected map[string]json.RawMessage
	for _, item := range original.Layers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var tag struct {
			ID uuid.UUID `json:"id"`
		}
		if json.Unmarshal(item, &tag) != nil {
			return nil, v2input.ErrInvalid
		}
		if tag.ID == source.Instruction.Layers[0] {
			if selected != nil {
				return nil, v2input.ErrInvalid
			}
			if json.Unmarshal(item, &selected) != nil {
				return nil, v2input.ErrInvalid
			}
		}
	}
	if selected == nil {
		return nil, v2input.ErrInvalid
	}
	var enabled bool
	var opacity float64
	var operation, version string
	if json.Unmarshal(selected["enabled"], &enabled) != nil || !enabled || json.Unmarshal(selected["opacity"], &opacity) != nil || opacity <= 0 || json.Unmarshal(selected["operation"], &operation) != nil || operation != "portrait.detail" || json.Unmarshal(selected["operation_version"], &version) != nil || version != "1" {
		return nil, v2input.ErrInvalid
	}
	mapping := make(map[v2input.Ref]v2input.Ref, len(bindings))
	used := make(map[v2input.Ref]bool, len(bindings))
	for _, binding := range bindings {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !validLayerArtifactRef(binding.Source) || !validLayerArtifactRef(binding.Current) {
			return nil, v2input.ErrInvalid
		}
		if _, exists := mapping[binding.Source]; exists {
			return nil, v2input.ErrInvalid
		}
		mapping[binding.Source] = binding.Current
	}
	nodes := 0
	var project func(json.RawMessage, int) (json.RawMessage, error)
	project = func(raw json.RawMessage, depth int) (json.RawMessage, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		nodes++
		if depth > 16 || nodes > 256 {
			return nil, v2input.ErrInvalid
		}
		var node map[string]json.RawMessage
		var op string
		if json.Unmarshal(raw, &node) != nil || json.Unmarshal(node["op"], &op) != nil {
			return nil, v2input.ErrInvalid
		}
		switch op {
		case "ref":
			if len(node) != 3 {
				return nil, v2input.ErrInvalid
			}
			var ref v2input.Ref
			if json.Unmarshal(node["mask"], &ref) != nil {
				return nil, v2input.ErrInvalid
			}
			current, exists := mapping[ref]
			if !exists {
				return nil, v2input.ErrInvalid
			}
			used[ref] = true
			return canonical.Marshal(map[string]any{"op": "ref", "mask": current, "canvas_hash": source.Source.CanvasHash})
		case "invert":
			if len(node) != 2 {
				return nil, v2input.ErrInvalid
			}
			child, err := project(node["operand"], depth+1)
			if err != nil {
				return nil, err
			}
			return canonical.Marshal(map[string]any{"op": op, "operand": child})
		case "union", "intersect", "subtract":
			if len(node) != 3 {
				return nil, v2input.ErrInvalid
			}
			left, err := project(node["left"], depth+1)
			if err != nil {
				return nil, err
			}
			right, err := project(node["right"], depth+1)
			if err != nil {
				return nil, err
			}
			return canonical.Marshal(map[string]any{"op": op, "left": left, "right": right})
		default:
			return nil, v2input.ErrInvalid
		}
	}
	mask, err := project(selected["mask"], 0)
	if err != nil {
		return nil, err
	}
	if len(used) != len(mapping) {
		return nil, v2input.ErrInvalid
	}
	selected["mask"] = mask
	selected["params"], err = canonical.Marshal(params)
	if err != nil {
		return nil, err
	}
	layer, err := canonical.Marshal(selected)
	if err != nil {
		return nil, err
	}
	result, err := canonical.Marshal(map[string]any{"schema_version": 1, "source": source.Source, "layers": []json.RawMessage{layer}})
	if err != nil {
		return nil, err
	}
	if _, err = v2draft.ValidateLayerStack(result, source.Source); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
