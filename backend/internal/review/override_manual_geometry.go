package review

import (
	"encoding/json"
	"errors"
	"math"

	"github.com/google/uuid"
)

// ValidateOverrideManualGeometry applies the same normalized polygon and
// Face68 contracts used by manual-mask rasterization and landmark review.
// Source ownership and producer consent remain separate authorities.
func ValidateOverrideManualGeometry(polygons []json.RawMessage, landmarks json.RawMessage, sourceSHA string) error {
	fail := func() error { return errors.New("invalid override manual geometry") }
	if len(polygons) > 128 || len(sourceSHA) != 64 {
		return fail()
	}
	seen := map[int64]bool{}
	points := 0
	unit := func(value float64) bool {
		return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
	}
	for _, raw := range polygons {
		var shape manualShape
		if decodeExact(raw, &shape) != nil || shape.Kind != "polygon" || shape.Mode != "protect" || shape.Sequence < 0 || shape.Sequence > 2147483647 || seen[shape.Sequence] || shape.Radius != nil || shape.Hardness != nil || len(shape.Points) < 3 || len(shape.Points) > 4096 {
			return fail()
		}
		if !manualPolygonFieldsPresent(raw) {
			return fail()
		}
		seen[shape.Sequence] = true
		points += len(shape.Points)
		if points > 10000 {
			return fail()
		}
		for _, point := range shape.Points {
			if !unit(point.X) || !unit(point.Y) {
				return fail()
			}
		}
	}
	if len(landmarks) == 0 || string(landmarks) == "null" {
		return nil
	}
	var face Face68
	if decodeExact(landmarks, &face) != nil || !manualFaceFieldsPresent(landmarks) || face.ObjectID == uuid.Nil || len(face.Points) != 68 || !validCanvas(face.Canvas) || face.Canvas.SourceSHA256 != sourceSHA || validateTransformChain(face.TransformChain, face.Canvas) != nil {
		return fail()
	}
	for _, point := range face.Points {
		if !unit(point.X) || !unit(point.Y) || !unit(point.Confidence) {
			return fail()
		}
	}
	return nil
}

func manualFaceFieldsPresent(raw []byte) bool {
	var face map[string]json.RawMessage
	if json.Unmarshal(raw, &face) != nil || len(face) != 4 {
		return false
	}
	for _, name := range []string{"object_id", "points", "canvas", "transform_chain"} {
		if len(face[name]) == 0 || string(face[name]) == "null" {
			return false
		}
	}
	var points []map[string]json.RawMessage
	if json.Unmarshal(face["points"], &points) != nil || len(points) != 68 {
		return false
	}
	for _, point := range points {
		if len(point) != 3 {
			return false
		}
		for _, name := range []string{"x", "y", "confidence"} {
			if len(point[name]) == 0 || string(point[name]) == "null" {
				return false
			}
		}
	}
	return true
}

func manualPolygonFieldsPresent(raw []byte) bool {
	var shape map[string]json.RawMessage
	if json.Unmarshal(raw, &shape) != nil || len(shape) != 4 {
		return false
	}
	for _, name := range []string{"kind", "sequence", "mode", "points"} {
		if len(shape[name]) == 0 || string(shape[name]) == "null" {
			return false
		}
	}
	var points []map[string]json.RawMessage
	if json.Unmarshal(shape["points"], &points) != nil {
		return false
	}
	for _, point := range points {
		if len(point) != 2 || len(point["x"]) == 0 || len(point["y"]) == 0 || string(point["x"]) == "null" || string(point["y"]) == "null" {
			return false
		}
	}
	return true
}
