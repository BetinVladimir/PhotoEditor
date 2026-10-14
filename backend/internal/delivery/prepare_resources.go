package delivery

import (
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type PrepareResourceLimits struct{ RAMBytes, ScratchBytes, OutputBytes int64 }
type prepareResourceEstimate struct {
	RAMBytes     int64 `json:"ram_bytes"`
	VRAMBytes    int64 `json:"vram_bytes"`
	ScratchBytes int64 `json:"scratch_bytes"`
	OutputBytes  int64 `json:"output_bytes"`
	PixelCount   int64 `json:"pixel_count"`
}

// prepareProfile reserves JSON parsing/model/canonicalization copies, runtime
// overhead and the input inventory. Inputs are documents: no pixel buffers or
// GPU memory are needed. These conservative reservations require calibration
// later; they are not measured peak-memory claims.
func (p PreparePlan) prepareProfile(entries int, limits PrepareResourceLimits) (json.RawMessage, error) {
	if p.task == [16]byte{} || p.documentBytes < 1 || p.documentBytes > 26<<20 || entries < 1 || entries > 1000000 {
		return nil, v2input.ErrInvalid
	}
	estimate := prepareResourceEstimate{
		RAMBytes:     (512 << 20) + 32*p.documentBytes + int64(entries)*1024,
		VRAMBytes:    0,
		ScratchBytes: (32 << 20) + p.documentBytes + int64(entries)*1024,
		OutputBytes:  8 << 20,
		PixelCount:   0,
	}
	if limits.RAMBytes < estimate.RAMBytes || limits.ScratchBytes < estimate.ScratchBytes || limits.OutputBytes < estimate.OutputBytes {
		return nil, v2input.ErrInvalid
	}
	return canonical.Marshal(struct {
		Backend          string                  `json:"backend"`
		RendererVersion  string                  `json:"renderer_version"`
		ResourceEstimate prepareResourceEstimate `json:"resource_estimate"`
	}{"cpu", "v2.2", estimate})
}

// DraftTask supplies the blocked shell using the same pinned inventory as later
// transport publication. Current ACL and complete dependency resolution must
// already have been established by the caller. No SQL writes occur here.
func (p PreparePlan) DraftTask(resolved []v2input.Entry, limits PrepareResourceLimits) (v2input.DraftTask, error) {
	frozen, err := p.FreezeTask(resolved, limits)
	if err != nil {
		return v2input.DraftTask{}, err
	}
	return frozen.DraftTask()
}
