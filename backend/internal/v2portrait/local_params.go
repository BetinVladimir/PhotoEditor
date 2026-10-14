package v2portrait

import (
	"github.com/google/uuid"
	"math"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// Controls are authored request values; compilation is not artifact admission.
type HairParams struct {
	SchemaVersion    int                           `json:"schema_version"`
	Base             v2input.SessionVersionBinding `json:"base"`
	Strength         float64                       `json:"strength"`
	Mask             v2input.Ref                   `json:"mask"`
	HueDeltaDegrees  float64                       `json:"hue_delta_degrees"`
	SaturationFactor float64                       `json:"saturation_factor"`
	Detail           float64                       `json:"detail"`
	Protected        v2input.Ref                   `json:"protected"`
}

type ClothingParams struct {
	SchemaVersion    int                           `json:"schema_version"`
	Base             v2input.SessionVersionBinding `json:"base"`
	Strength         float64                       `json:"strength"`
	Mask             v2input.Ref                   `json:"mask"`
	Protected        v2input.Ref                   `json:"protected"`
	HueDeltaDegrees  float64                       `json:"hue_delta_degrees"`
	SaturationFactor float64                       `json:"saturation_factor"`
	WrinkleStrength  float64                       `json:"wrinkle_strength"`
}

func finiteRange(n, lo, hi float64) bool {
	return !math.IsNaN(n) && !math.IsInf(n, 0) && n >= lo && n <= hi
}
func validLocalBase(version int, base v2input.SessionVersionBinding, mask, guard v2input.Ref, strength float64) bool {
	if version != 1 || base.AssetVersionID == uuid.Nil || !finiteRange(strength, 0, 1) {
		return false
	}
	if _, err := domain.ParseDigest(base.CanvasHash); err != nil {
		return false
	}
	for _, ref := range []v2input.Ref{base.Source, mask, guard} {
		if ref.ArtifactID == uuid.Nil {
			return false
		}
		if _, err := domain.ParseDigest(ref.SHA256); err != nil {
			return false
		}
	}
	return true
}
func compileParams(value any) ([]byte, string, error) {
	body, err := canonical.Marshal(value)
	if err != nil {
		return nil, "", err
	}
	sha, err := canonical.Hash(value)
	if err != nil {
		return nil, "", err
	}
	return body, sha, nil
}
func (p HairParams) Compile() ([]byte, string, error) {
	if !validLocalBase(p.SchemaVersion, p.Base, p.Mask, p.Protected, p.Strength) || !finiteRange(p.HueDeltaDegrees, -30, 30) || !finiteRange(p.SaturationFactor, .7, 1.3) || !finiteRange(p.Detail, 0, 1) {
		return nil, "", v2input.ErrInvalid
	}
	return compileParams(p)
}
func (p ClothingParams) Compile() ([]byte, string, error) {
	if !validLocalBase(p.SchemaVersion, p.Base, p.Mask, p.Protected, p.Strength) || !finiteRange(p.HueDeltaDegrees, -180, 180) || !finiteRange(p.SaturationFactor, 0, 2) || !finiteRange(p.WrinkleStrength, 0, 1) {
		return nil, "", v2input.ErrInvalid
	}
	return compileParams(p)
}
