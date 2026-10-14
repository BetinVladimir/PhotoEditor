package v2draft

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func manualRasterParamRefs(raw []byte, path string, parent v2input.Ref) ([]LayerRef, bool, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil, false, v2input.ErrInvalid
	}
	_, image := fields["image"]
	_, external := fields["vectors"]
	_, inline := fields["inline_vectors"]
	if !image || (!external && !inline) {
		return nil, false, nil
	}
	invalid := func() ([]LayerRef, bool, error) { return nil, true, v2input.ErrInvalid }
	if _, err := canonical.Bytes(raw); err != nil || len(raw) > 1048576 {
		return invalid()
	}
	for key := range fields {
		if key != "image" && key != "vectors" && key != "inline_vectors" && key != "object_id" {
			return invalid()
		}
	}
	source, ok := layerRef(fields["image"])
	if !ok {
		return invalid()
	}
	hasExternal := external && string(fields["vectors"]) != "null"
	hasInline := inline && string(fields["inline_vectors"]) != "null"
	if hasExternal == hasInline {
		return invalid()
	}
	if body, exists := fields["object_id"]; exists && string(body) != "null" {
		var id uuid.UUID
		if json.Unmarshal(body, &id) != nil || id == uuid.Nil {
			return invalid()
		}
	}
	children := []LayerRef{{Ref: source, Contract: "WorkingImage", Path: path + ".image", ParentRef: parent}}
	if hasExternal {
		vector, ok := layerRef(fields["vectors"])
		if !ok {
			return invalid()
		}
		children = append(children, LayerRef{Ref: vector, Contract: "ManualMask", Path: path + ".vectors", ParentRef: parent})
	} else {
		canvas, err := review.ParseManualVectorsCanvas(fields["inline_vectors"])
		if err != nil {
			return invalid()
		}
		hash, err := canonical.Hash(canvas)
		if err != nil {
			return invalid()
		}
		children[0].CanvasHash = hash
	}
	return children, true, nil
}
