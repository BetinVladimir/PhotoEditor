package v2render

import (
	"encoding/json"
	"math"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Estimates are not measurements. The operation's independently trusted measured
// CPU report must fit the actual installed compiler profile; never use parser
// model costs, synthetic peaks or GPU capacity as this mask's qualification.
func authoredSkinMeasuredBudget(raw json.RawMessage, report v2qualification.Report) error {
	var profile struct {
		Backend  string `json:"backend"`
		Renderer string `json:"renderer_version"`
		Estimate struct {
			RAM     int64 `json:"ram_bytes"`
			VRAM    int64 `json:"vram_bytes"`
			Scratch int64 `json:"scratch_bytes"`
			Output  int64 `json:"output_bytes"`
			Pixels  int64 `json:"pixel_count"`
		} `json:"resource_estimate"`
	}
	if json.Unmarshal(raw, &profile) != nil || profile.Backend != "cpu" || profile.Renderer != "v2.2" || profile.Estimate.VRAM != 0 || profile.Estimate.RAM < 1 || profile.Estimate.Scratch < 1 || profile.Estimate.Output < 1 || profile.Estimate.Pixels < 1 || report.Operation != "mask.face_skin" || report.Version != "1" || report.Metrics.PeakVRAMBytes != 0 {
		return v2input.ErrInvalid
	}
	for _, pair := range []struct {
		measured float64
		limit    int64
	}{{report.Metrics.PeakRSSBytes, profile.Estimate.RAM}, {report.Metrics.ScratchBytes, profile.Estimate.Scratch}} {
		if math.IsNaN(pair.measured) || math.IsInf(pair.measured, 0) || pair.measured < 0 || pair.measured > float64(pair.limit) {
			return v2input.ErrInvalid
		}
	}
	return nil
}

// Input-index accounting is opt-in after the legacy compiler; it must not exceed
// the caller's original deployment limits when added.
func authoredSkinProfileWithinLimits(raw json.RawMessage, limits AuthoredFaceBudget) error {
	var p struct {
		Estimate struct {
			RAM     int64 `json:"ram_bytes"`
			VRAM    int64 `json:"vram_bytes"`
			Scratch int64 `json:"scratch_bytes"`
			Output  int64 `json:"output_bytes"`
		} `json:"resource_estimate"`
	}
	if json.Unmarshal(raw, &p) != nil || limits.Backend != "cpu" || limits.VRAM != 0 || p.Estimate.VRAM != 0 || p.Estimate.RAM < 1 || p.Estimate.Scratch < 1 || p.Estimate.Output < 1 || p.Estimate.RAM > limits.RAM || p.Estimate.Scratch > limits.Scratch || p.Estimate.Output > limits.Output {
		return v2input.ErrInvalid
	}
	return nil
}
