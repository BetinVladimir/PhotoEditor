package v2optical

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// Frames retain request order; neither sorting nor omission is permitted.
type PanoramaParams struct {
	SchemaVersion    int                             `json:"schema_version"`
	Frames           []v2input.SessionVersionBinding `json:"frames"`
	ReferenceOrdinal int                             `json:"reference_ordinal"`
	MaxPixels        int                             `json:"max_pixels"`
	Calibration      v2input.Ref                     `json:"calibration"`
	Projection       string                          `json:"projection"`
}
type FocusStackParams struct {
	SchemaVersion    int                             `json:"schema_version"`
	Frames           []v2input.SessionVersionBinding `json:"frames"`
	ReferenceOrdinal int                             `json:"reference_ordinal"`
	MaxPixels        int                             `json:"max_pixels"`
	Calibration      v2input.Ref                     `json:"calibration"`
	MotionPolicy     string                          `json:"motion_policy"`
}

func validateMultiParams(schema int, frames []v2input.SessionVersionBinding, reference, maxPixels, limit int, calibration v2input.Ref) error {
	if schema != 1 || len(frames) < 2 || len(frames) > limit || reference < 0 || reference >= len(frames) ||
		maxPixels < 1 || maxPixels > 200000000 || !validPerspectiveRef(calibration) {
		return v2input.ErrInvalid
	}
	versions := map[uuid.UUID]bool{}
	for _, frame := range frames {
		if frame.AssetVersionID == uuid.Nil || versions[frame.AssetVersionID] || !validPerspectiveRef(frame.Source) ||
			frame.Source.ArtifactID == calibration.ArtifactID {
			return v2input.ErrInvalid
		}
		if _, err := domain.ParseDigest(frame.CanvasHash); err != nil {
			return v2input.ErrInvalid
		}
		versions[frame.AssetVersionID] = true
	}
	return nil
}
func compileMulti(value any) ([]byte, string, error) {
	body, err := canonical.Marshal(value)
	if err != nil || len(body) > 65536 {
		return nil, "", v2input.ErrInvalid
	}
	digest, err := canonical.Hash(value)
	if err != nil {
		return nil, "", v2input.ErrInvalid
	}
	return body, digest, nil
}
func (p PanoramaParams) Compile() ([]byte, string, error) {
	if validateMultiParams(p.SchemaVersion, p.Frames, p.ReferenceOrdinal, p.MaxPixels, 20, p.Calibration) != nil ||
		(p.Projection != "planar" && p.Projection != "cylindrical") {
		return nil, "", v2input.ErrInvalid
	}
	return compileMulti(p)
}
func (p FocusStackParams) Compile() ([]byte, string, error) {
	if validateMultiParams(p.SchemaVersion, p.Frames, p.ReferenceOrdinal, p.MaxPixels, 30, p.Calibration) != nil ||
		(p.MotionPolicy != "review" && p.MotionPolicy != "reject") {
		return nil, "", v2input.ErrInvalid
	}
	return compileMulti(p)
}
func ParsePanoramaParams(raw []byte) (PanoramaParams, error) {
	var p PanoramaParams
	if len(raw) < 2 || len(raw) > 65536 || json.Unmarshal(raw, &p) != nil {
		return PanoramaParams{}, v2input.ErrInvalid
	}
	body, _, err := p.Compile()
	if err != nil || !bytes.Equal(body, raw) {
		return PanoramaParams{}, v2input.ErrInvalid
	}
	return p, nil
}
func ParseFocusStackParams(raw []byte) (FocusStackParams, error) {
	var p FocusStackParams
	if len(raw) < 2 || len(raw) > 65536 || json.Unmarshal(raw, &p) != nil {
		return FocusStackParams{}, v2input.ErrInvalid
	}
	body, _, err := p.Compile()
	if err != nil || !bytes.Equal(body, raw) {
		return FocusStackParams{}, v2input.ErrInvalid
	}
	return p, nil
}
