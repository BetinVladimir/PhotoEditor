package v2draft

import (
	"encoding/json"
	"photoeditor/backend/internal/v2input"
)

// compositionParamRefs preserves declared contracts in the layer.compose wire
// rather than recursively treating its already typed roots as generic Artifact.
func compositionParamRefs(raw []byte, path string, parent v2input.Ref) ([]LayerRef, bool, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil, false, v2input.ErrInvalid
	}
	if _, ok := fields["proposal_result"]; !ok {
		return nil, false, nil
	}
	invalid := func() ([]LayerRef, bool, error) { return nil, true, v2input.ErrInvalid }
	if len(fields) != 8 {
		return invalid()
	}
	for _, key := range []string{"schema_version", "base", "source_result", "proposal_result", "mask", "protected_mask", "opacity", "blend"} {
		if _, ok := fields[key]; !ok {
			return invalid()
		}
	}
	if string(fields["schema_version"]) != "1" {
		return invalid()
	}
	var opacity float64
	var blend string
	if string(fields["opacity"]) == "null" || json.Unmarshal(fields["opacity"], &opacity) != nil || opacity < 0 || opacity > 1 || json.Unmarshal(fields["blend"], &blend) != nil || (blend != "normal" && blend != "multiply" && blend != "screen") {
		return invalid()
	}
	var base map[string]json.RawMessage
	if json.Unmarshal(fields["base"], &base) != nil || len(base) != 3 {
		return invalid()
	}
	var binding v2input.SessionVersionBinding
	if json.Unmarshal(fields["base"], &binding) != nil || !isDocumentRef(binding.Source) || !isDocumentRef(v2input.Ref{ArtifactID: binding.AssetVersionID, SHA256: binding.CanvasHash}) {
		return invalid()
	}
	children := []LayerRef{{Ref: binding.Source, Contract: "WorkingImage", ParentRef: parent, Path: path + ".base.source", CanvasHash: binding.CanvasHash}}
	add := func(body json.RawMessage, contract, key string) bool {
		ref, ok := layerRef(body)
		if !ok {
			return false
		}
		children = append(children, LayerRef{Ref: ref, Contract: contract, ParentRef: parent, Path: key})
		return true
	}
	if !add(fields["proposal_result"], "ImageResult", path+".proposal_result") {
		return invalid()
	}
	if string(fields["source_result"]) != "null" && !add(fields["source_result"], "ImageResult", path+".source_result") {
		return invalid()
	}
	if string(fields["protected_mask"]) != "null" && !add(fields["protected_mask"], "MaskRef", path+".protected_mask") {
		return invalid()
	}
	visits := 0
	var walk func(json.RawMessage, string, int) bool
	walk = func(body json.RawMessage, key string, depth int) bool {
		visits++
		if visits > 256 || depth > 16 {
			return false
		}
		var node map[string]json.RawMessage
		var op string
		if json.Unmarshal(body, &node) != nil || json.Unmarshal(node["op"], &op) != nil {
			return false
		}
		switch op {
		case "ref":
			var canvas string
			return len(node) == 3 && json.Unmarshal(node["canvas_hash"], &canvas) == nil && canvas == binding.CanvasHash && add(node["mask"], "MaskRef", key+".mask")
		case "invert":
			return len(node) == 2 && walk(node["operand"], key+".operand", depth+1)
		case "union", "intersect", "subtract":
			return len(node) == 3 && walk(node["left"], key+".left", depth+1) && walk(node["right"], key+".right", depth+1)
		default:
			return false
		}
	}
	if !walk(fields["mask"], path+".mask", 0) {
		return invalid()
	}
	return children, true, nil
}
