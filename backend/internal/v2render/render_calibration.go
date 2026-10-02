package v2render

import (
	"context"
	"math/big"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// RenderCalibrationCatalog is an operator-owned verified registry, never client
// request data. Implementations must authenticate measured evidence and pins.
type RenderCalibrationCatalog interface {
	Resolve(context.Context, RenderQualification) (RenderCalibration, error)
}

type RenderCalibrationSample struct {
	Width    int64              `json:"width"`
	Height   int64              `json:"height"`
	Provider RenderProviderPeak `json:"provider_peak"`
}

// Each sample measures provider overhead, excluding RenderResources orchestration.
// Qualification identifies model/runtime/backend/device/dataset exactly.
type RenderCalibration struct {
	Qualification RenderQualification       `json:"qualification"`
	EvidenceSHA   string                    `json:"evidence_sha"`
	ICCSHA        string                    `json:"icc_lock_sha"`
	TileSize      int                       `json:"tile_size"`
	AlphaMode     string                    `json:"alpha_mode"`
	Samples       []RenderCalibrationSample `json:"samples"`
}

func renderUnavailable() error { return domain.NewError("RENDER_UNAVAILABLE", 503) }

func (c RenderCalibration) validate(q RenderQualification) error {
	if c.Qualification != q || len(c.Samples) < 1 || len(c.Samples) > 256 {
		return renderUnavailable()
	}
	for _, pin := range []string{q.ModelSHA, q.RuntimeSHA, q.DeviceSHA, q.DatasetSHA, c.EvidenceSHA, c.ICCSHA} {
		if _, err := domain.ParseDigest(pin); err != nil {
			return renderUnavailable()
		}
	}
	choice := RenderExecutionChoice{Backend: q.Backend, RuntimeSHA: q.RuntimeSHA, TileSize: c.TileSize, AlphaMode: c.AlphaMode}
	if choice.validate() != nil {
		return renderUnavailable()
	}
	seen := map[[2]int64]bool{}
	for _, sample := range c.Samples {
		dimensions := [2]int64{sample.Width, sample.Height}
		if seen[dimensions] {
			return renderUnavailable()
		}
		seen[dimensions] = true
		if _, err := RenderResources(sample.Width, sample.Height, 0, 0, c.TileSize, c.AlphaMode, sample.Provider); err != nil {
			return renderUnavailable()
		}
	}
	return nil
}

// Select bounds both axes without rotating/extrapolating GPU measurements.
// The smallest covering area wins; equal areas use width then height, so registry
// order cannot change the frozen budget. RAM/VRAM retain measured upper bounds;
// scratch/output scale by pixel count with exact ceil(1.2 * ratio).
func (c RenderCalibration) Select(q RenderQualification, width, height int64, iccSHA string) (RenderExecutionChoice, error) {
	if err := c.validate(q); err != nil {
		return RenderExecutionChoice{}, err
	}
	if width < 1 || height < 1 || width > 200000000 || height > 200000000/width {
		return RenderExecutionChoice{}, v2input.ErrInvalid
	}
	if iccSHA != c.ICCSHA {
		return RenderExecutionChoice{}, renderUnavailable()
	}
	var selected *RenderCalibrationSample
	for i := range c.Samples {
		sample := &c.Samples[i]
		if sample.Width < width || sample.Height < height {
			continue
		}
		if selected == nil || sample.Width*sample.Height < selected.Width*selected.Height ||
			(sample.Width*sample.Height == selected.Width*selected.Height && sample.Width < selected.Width) {
			selected = sample
		}
	}
	if selected == nil {
		return RenderExecutionChoice{}, renderUnavailable()
	}
	peak := selected.Provider
	scale := func(value int64) (int64, error) {
		numerator := new(big.Int).Mul(big.NewInt(value), big.NewInt(width*height))
		numerator.Mul(numerator, big.NewInt(6))
		denominator := big.NewInt(selected.Width * selected.Height * 5)
		numerator.Add(numerator, new(big.Int).Sub(denominator, big.NewInt(1)))
		numerator.Quo(numerator, denominator)
		if !numerator.IsInt64() || numerator.Int64() > 9007199254740991 {
			return 0, renderUnavailable()
		}
		return numerator.Int64(), nil
	}
	var err error
	if peak.ScratchBytes, err = scale(peak.ScratchBytes); err != nil {
		return RenderExecutionChoice{}, err
	}
	if peak.OutputBytes, err = scale(peak.OutputBytes); err != nil {
		return RenderExecutionChoice{}, err
	}
	choice := RenderExecutionChoice{Backend: q.Backend, RuntimeSHA: q.RuntimeSHA, TileSize: c.TileSize, AlphaMode: c.AlphaMode, Provider: peak}
	if choice.validate() != nil {
		return RenderExecutionChoice{}, renderUnavailable()
	}
	return choice, nil
}
