package v2render

import (
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// RenderQualificationPin freezes the actual measured profile selected during
// preparation. CalibrationSHA is the canonical complete profile hash.
type RenderQualificationPin struct {
	RenderQualification
	CalibrationSHA string `json:"calibration_sha"`
}

func (p RenderQualificationPin) validate() error {
	for _, digest := range []string{p.ModelSHA, p.RuntimeSHA, p.DeviceSHA, p.DatasetSHA, p.CalibrationSHA} {
		if _, err := domain.ParseDigest(digest); err != nil {
			return v2input.ErrInvalid
		}
	}
	if p.Backend != "cpu" && p.Backend != "cuda" && p.Backend != "mps" {
		return v2input.ErrInvalid
	}
	return nil
}
