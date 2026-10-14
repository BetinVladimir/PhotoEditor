package v2render

import (
	"bytes"
	"encoding/json"
	"math"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

func validatePerspectiveResolvedTransform(raw []byte) error {
	var document struct {
		SchemaVersion int       `json:"schema_version"`
		From          string    `json:"from_canvas_hash"`
		To            string    `json:"to_canvas_hash"`
		Matrix        []float64 `json:"matrix"`
		Kind          string    `json:"kind"`
	}
	if len(raw) < 2 || len(raw) > 65536 || json.Unmarshal(raw, &document) != nil || document.SchemaVersion != 1 || document.Kind != "homography" || len(document.Matrix) != 9 {
		return v2input.ErrInvalid
	}
	normal, err := canonical.Bytes(raw)
	if err != nil {
		return err
	}
	exact, err := canonical.Marshal(document)
	if err != nil || !bytes.Equal(exact, normal) {
		return v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(document.From); err != nil {
		return v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(document.To); err != nil {
		return v2input.ErrInvalid
	}
	for _, v := range document.Matrix {
		if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 1e8 {
			return v2input.ErrInvalid
		}
	}
	flat := document.Matrix
	m := [3][3]float64{{flat[0], flat[1], flat[2]}, {flat[3], flat[4], flat[5]}, {flat[6], flat[7], flat[8]}}
	det := m[0][0]*(m[1][1]*m[2][2]-m[1][2]*m[2][1]) - m[0][1]*(m[1][0]*m[2][2]-m[1][2]*m[2][0]) + m[0][2]*(m[1][0]*m[2][1]-m[1][1]*m[2][0])
	if math.Abs(det) <= 1e-10 || math.IsNaN(det) || math.IsInf(det, 0) {
		return v2input.ErrInvalid
	}
	return nil
}
