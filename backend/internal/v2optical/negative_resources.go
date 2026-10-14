package v2optical

import "photoeditor/backend/internal/v2input"

// NegativeResourceEstimate matches conservative CPU worker admission.
// Actual capacity, qualification and source authority are separate gates.
type NegativeResourceEstimate struct {
	PixelCount           int64 `json:"pixel_count"`
	VRAMBytes            int64 `json:"vram_bytes"`
	RAMBytes             int64 `json:"ram_bytes"`
	MaterialScratchBytes int64 `json:"material_scratch_bytes"`
	OutputScratchBytes   int64 `json:"output_scratch_bytes"`
	ScratchBytes         int64 `json:"scratch_bytes"`
	OutputBytes          int64 `json:"output_bytes"`
}

func NegativeResources(width, height int64, hasAlpha bool) (NegativeResourceEstimate, error) {
	if width < 1 || height < 1 || width > 200000000 || height > 200000000/width {
		return NegativeResourceEstimate{}, v2input.ErrInvalid
	}
	pixels := width * height
	owners, materialOwners := int64(28), int64(12)
	if hasAlpha {
		owners += 4
		materialOwners += 4
	}
	tile := pixels
	if tile > 512*512 {
		tile = 512 * 512
	}
	material, output := materialOwners*pixels+32768, 16*pixels+32768
	return NegativeResourceEstimate{PixelCount: pixels, RAMBytes: owners*pixels + 128*tile + 512*1024*1024,
		MaterialScratchBytes: material, OutputScratchBytes: output, ScratchBytes: material + output,
		OutputBytes: 16*pixels + 32768 + 4*65536}, nil
}
