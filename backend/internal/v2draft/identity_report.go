package v2draft

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"math"
	"photoeditor/backend/internal/v2input"
)

func identityReportRefs(raw []byte, path string, parent v2input.Ref) ([]LayerRef, error) {
	f, ok := exactFields(raw, "schema_version", "status", "faces", "changed_mask", "generated_mask", "outside_mask_max_error", "calibration")
	invalid := func() ([]LayerRef, error) { return nil, v2input.ErrInvalid }
	if !ok || !isDocumentRef(parent) {
		return invalid()
	}
	var version int
	var status string
	var faces []json.RawMessage
	if json.Unmarshal(f["schema_version"], &version) != nil || version != 1 || json.Unmarshal(f["status"], &status) != nil || (status != "pass" && status != "review" && status != "fail" && status != "not_measurable") || json.Unmarshal(f["faces"], &faces) != nil || faces == nil || len(faces) > 100 {
		return invalid()
	}
	null := func(raw []byte) bool { return bytes.Equal(bytes.TrimSpace(raw), []byte("null")) }
	metric := func(raw []byte, low, high float64) bool {
		var value float64
		return !null(raw) && json.Unmarshal(raw, &value) == nil && !math.IsNaN(value) && !math.IsInf(value, 0) && value >= low && value <= high
	}
	if !metric(f["outside_mask_max_error"], 0, 1000000) {
		return invalid()
	}
	for _, face := range faces {
		fields, valid := exactFields(face, "face_id", "similarity", "landmark_drift")
		if !valid {
			return invalid()
		}
		var id uuid.UUID
		if null(fields["face_id"]) || json.Unmarshal(fields["face_id"], &id) != nil {
			return invalid()
		}
		if !null(fields["similarity"]) && !metric(fields["similarity"], -1, 1) {
			return invalid()
		}
		if !null(fields["landmark_drift"]) && !metric(fields["landmark_drift"], 0, 1) {
			return invalid()
		}
	}
	children := []LayerRef{}
	for _, field := range []struct{ name, contract string }{{"changed_mask", "MaskRef"}, {"generated_mask", "MaskRef"}, {"calibration", "Calibration"}} {
		if field.name == "generated_mask" && null(f[field.name]) {
			continue
		}
		ref, valid := layerRef(f[field.name])
		if !valid {
			return invalid()
		}
		children = append(children, LayerRef{Path: path + "." + field.name, Contract: field.contract, Ref: ref, ParentRef: parent})
	}
	return children, nil
}
