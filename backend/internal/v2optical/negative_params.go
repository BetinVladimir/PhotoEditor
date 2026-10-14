package v2optical

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"math"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

type NegativeParams struct {
	SchemaVersion int                           `json:"schema_version"`
	Base          v2input.SessionVersionBinding `json:"base"`
	Strength      float64                       `json:"strength"`
	FilmBaseRGB   [3]float64                    `json:"film_base_rgb"`
	Calibration   v2input.Ref                   `json:"calibration"`
	ExposureEV    float64                       `json:"exposure_ev"`
}

func negativeRange(n, lo, hi float64) bool {
	return !math.IsNaN(n) && !math.IsInf(n, 0) && n >= lo && n <= hi
}
func (p NegativeParams) Compile() ([]byte, string, error) {
	invalid := func() ([]byte, string, error) { return nil, "", v2input.ErrInvalid }
	if p.SchemaVersion != 1 || p.Base.AssetVersionID == uuid.Nil || !negativeRange(p.Strength, 0, 1) || !negativeRange(p.ExposureEV, -3, 3) {
		return invalid()
	}
	if _, err := domain.ParseDigest(p.Base.CanvasHash); err != nil {
		return invalid()
	}
	for _, ref := range []v2input.Ref{p.Base.Source, p.Calibration} {
		if ref.ArtifactID == uuid.Nil {
			return invalid()
		}
		if _, err := domain.ParseDigest(ref.SHA256); err != nil {
			return invalid()
		}
	}
	for _, n := range p.FilmBaseRGB {
		if !negativeRange(n, 1e-6, 1) {
			return invalid()
		}
	}
	body, err := canonical.Marshal(p)
	if err != nil {
		return invalid()
	}
	digest, err := canonical.Hash(p)
	if err != nil {
		return invalid()
	}
	return body, digest, nil
}

// ParseNegativeParams accepts only complete canonical original Params bytes.
// Compilation does not establish artifact, qualification or source authority.
func ParseNegativeParams(raw []byte) (NegativeParams, error) {
	var p NegativeParams
	if len(raw) < 2 || len(raw) > 65536 {
		return p, v2input.ErrInvalid
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return NegativeParams{}, v2input.ErrInvalid
	}
	body, _, err := p.Compile()
	if err != nil || !bytes.Equal(body, raw) {
		return NegativeParams{}, v2input.ErrInvalid
	}
	return p, nil
}
