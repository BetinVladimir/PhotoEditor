package v2draft

import (
	"bytes"
	"encoding/json"
	"photoeditor/backend/internal/v2input"
)

// imageResultRefs validates the complete ImageResult wire shape before exposing
// typed children. Generated lineage is required-nullable and cannot be omitted.
func imageResultRefs(raw []byte, path string, parent v2input.Ref) ([]LayerRef, error) {
	fields, ok := exactFields(raw, "schema_version", "working_image", "changed_mask", "report", "generated_mask")
	if !ok || !isDocumentRef(parent) {
		return nil, v2input.ErrInvalid
	}
	var version int
	if json.Unmarshal(fields["schema_version"], &version) != nil || version != 1 {
		return nil, v2input.ErrInvalid
	}
	children := []LayerRef{}
	for _, field := range []struct{ name, contract string }{{"working_image", "WorkingImage"}, {"changed_mask", "MaskRef"}, {"report", "ImageReport"}, {"generated_mask", "MaskRef"}} {
		value := fields[field.name]
		if field.name == "generated_mask" && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			continue
		}
		ref, valid := layerRef(value)
		if !valid {
			return nil, v2input.ErrInvalid
		}
		children = append(children, LayerRef{Path: path + "." + field.name, Contract: field.contract, Ref: ref, ParentRef: parent})
	}
	return children, nil
}

// ImageResultDocument retains the exact typed parent result references.
type ImageResultDocument struct {
	SchemaVersion int          `json:"schema_version"`
	WorkingImage  v2input.Ref  `json:"working_image"`
	ChangedMask   v2input.Ref  `json:"changed_mask"`
	Report        v2input.Ref  `json:"report"`
	GeneratedMask *v2input.Ref `json:"generated_mask"`
}

func ParseImageResult(raw []byte, parent v2input.Ref) (ImageResultDocument, error) {
	if _, err := imageResultRefs(raw, "ImageResult", parent); err != nil {
		return ImageResultDocument{}, err
	}
	var document ImageResultDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		return ImageResultDocument{}, v2input.ErrInvalid
	}
	return document, nil
}
