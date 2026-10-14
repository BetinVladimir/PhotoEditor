package v2draft

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// RenderFrameAuthority is derived from a verified descriptor occurrence.
// It is backend-only provenance, never accepted from a client JSON field.
type RenderFrameAuthority struct {
	Root            v2input.Ref
	Descriptor      v2input.Ref
	SelectedVersion uuid.UUID
	Learning        FrozenRenderLearning
	ModelLockSHA    string
	Ordinal         int
}

type LayerRef struct {
	ManualVectors        *ManualRasterVectors
	Path                 string
	Contract             string
	Ref                  v2input.Ref
	CanvasHash           string
	ParentRef            v2input.Ref
	ExpectedBinding      *v2input.SessionVersionBinding
	ExpectedLearning     *FrozenRenderLearning
	ExpectedModelLockSHA string
	ExpectedBackground   *FrozenBackgroundAsset
	RenderFrame          *RenderFrameAuthority
}

func exactFields(raw []byte, keys ...string) (map[string]json.RawMessage, bool) {
	if _, err := canonical.Bytes(raw); err != nil {
		return nil, false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != len(keys) {
		return nil, false
	}
	for _, key := range keys {
		if _, ok := fields[key]; !ok {
			return nil, false
		}
	}
	return fields, true
}

func layerRef(raw []byte) (v2input.Ref, bool) {
	_, ok := exactFields(raw, "artifact_id", "sha256")
	if !ok {
		return v2input.Ref{}, false
	}
	var ref v2input.Ref
	if json.Unmarshal(raw, &ref) != nil || ref.ArtifactID == uuid.Nil ||
		len(ref.SHA256) != 64 {
		return v2input.Ref{}, false
	}
	for _, c := range ref.SHA256 {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return v2input.Ref{}, false
		}
	}
	return ref, true
}

func maskRefs(raw []byte, path, canvasHash string, depth int,
	nodes *int, refs *[]LayerRef) bool {
	*nodes = *nodes + 1
	if depth > 16 || *nodes > 256 || len(*refs) > 512 {
		return false
	}
	var tag struct {
		Op string `json:"op"`
	}
	if json.Unmarshal(raw, &tag) != nil {
		return false
	}
	switch tag.Op {
	case "ref":
		fields, ok := exactFields(raw, "op", "mask", "canvas_hash")
		if !ok {
			return false
		}
		var hash string
		if json.Unmarshal(fields["canvas_hash"], &hash) != nil || hash != canvasHash {
			return false
		}
		ref, ok := layerRef(fields["mask"])
		if ok {
			*refs = append(*refs, LayerRef{Path: path + ".mask", Contract: "MaskRef", Ref: ref})
		}
		return ok
	case "invert":
		fields, ok := exactFields(raw, "op", "operand")
		return ok && maskRefs(fields["operand"], path+".operand", canvasHash, depth+1, nodes, refs)
	case "union", "intersect", "subtract":
		fields, ok := exactFields(raw, "op", "left", "right")
		return ok && maskRefs(fields["left"], path+".left", canvasHash, depth+1, nodes, refs) &&
			maskRefs(fields["right"], path+".right", canvasHash, depth+1, nodes, refs)
	default:
		return false
	}
}

// ValidateLayerStack enforces the wire structure, source binding and mask
// geometry identity. It returns nested refs for the artifact closure walker.
func ValidateLayerStack(raw []byte, binding v2input.SessionVersionBinding) ([]LayerRef, error) {
	if len(raw) < 2 || len(raw) > 1048576 || binding.AssetVersionID == uuid.Nil ||
		binding.Source.ArtifactID == uuid.Nil || len(binding.CanvasHash) != 64 {
		return nil, v2input.ErrInvalid
	}
	fields, ok := exactFields(raw, "schema_version", "source", "layers")
	if !ok {
		return nil, v2input.ErrInvalid
	}
	sourceFields, ok := exactFields(fields["source"], "asset_version_id", "source", "canvas_hash")
	if !ok {
		return nil, v2input.ErrInvalid
	}
	if _, ok = layerRef(sourceFields["source"]); !ok {
		return nil, v2input.ErrInvalid
	}
	var version int
	var source v2input.SessionVersionBinding
	var layers []json.RawMessage
	if json.Unmarshal(fields["schema_version"], &version) != nil || version != 1 ||
		json.Unmarshal(fields["source"], &source) != nil || source != binding ||
		json.Unmarshal(fields["layers"], &layers) != nil || len(layers) > 32 ||
		!strings.HasPrefix(string(fields["layers"]), "[") {
		return nil, v2input.ErrInvalid
	}
	refs := make([]LayerRef, 0, 1+len(layers)*2)
	refs = append(refs, LayerRef{Path: "LayerStack.source.source",
		Contract: "WorkingImage", Ref: binding.Source, CanvasHash: binding.CanvasHash})
	maskNodes := 0
	seenIDs := map[uuid.UUID]bool{}
	photometricSeen := false
	for i, rawLayer := range layers {
		layer, valid := exactFields(rawLayer, "id", "enabled", "opacity", "blend",
			"operation", "operation_version", "params", "mask")
		if !valid || string(layer["enabled"]) == "null" || string(layer["opacity"]) == "null" {
			return nil, v2input.ErrInvalid
		}
		var id uuid.UUID
		var enabled bool
		var opacity float64
		var blend, operation, operationVersion string
		if json.Unmarshal(layer["id"], &id) != nil || id == uuid.Nil ||
			json.Unmarshal(layer["enabled"], &enabled) != nil ||
			json.Unmarshal(layer["opacity"], &opacity) != nil ||
			math.IsNaN(opacity) || opacity < 0 || opacity > 1 ||
			json.Unmarshal(layer["blend"], &blend) != nil ||
			(blend != "normal" && blend != "multiply" && blend != "screen") ||
			json.Unmarshal(layer["operation"], &operation) != nil ||
			len(operation) < 1 || len(operation) > 80 ||
			json.Unmarshal(layer["operation_version"], &operationVersion) != nil ||
			len(operationVersion) < 1 || len(operationVersion) > 20 {
			return nil, v2input.ErrInvalid
		}

		if seenIDs[id] {
			return nil, v2input.ErrInvalid
		}
		seenIDs[id] = true
		if enabled && opacity > 0 {
			geometry := operation == "optical.lens" || operation == "optical.perspective" || operation == "optical.panorama" || operation == "optical.focus_stack"
			if geometry && photometricSeen {
				return nil, v2input.ErrInvalid
			}
			if !geometry {
				photometricSeen = true
			}
		}
		params, valid := layerRef(layer["params"])
		if !valid {
			return nil, v2input.ErrInvalid
		}
		path := "LayerStack.layers[" + strconv.Itoa(i) + "]"
		refs = append(refs, LayerRef{Path: path + ".params", Contract: "Params", Ref: params})
		if !maskRefs(layer["mask"], path+".mask", binding.CanvasHash, 0, &maskNodes, &refs) || len(refs) > 512 {
			return nil, v2input.ErrInvalid
		}
	}
	return refs, nil
}
