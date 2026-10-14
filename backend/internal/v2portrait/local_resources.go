package v2portrait

import "photoeditor/backend/internal/v2input"

// LocalPortraitResources matches worker CPU preparation reservations. This
// conservative bound remains until target appliance qualification is complete.
// Scheduler must separately compare it with actual host and quota capacity.
type LocalResourceEstimate struct {
	PixelCount   int64 `json:"pixel_count"`
	RAMBytes     int64 `json:"ram_bytes"`
	ScratchBytes int64 `json:"scratch_bytes"`
	OutputBytes  int64 `json:"output_bytes"`
	VRAMBytes    int64 `json:"vram_bytes"`
}

func LocalPortraitResources(width, height int64) (LocalResourceEstimate, error) {
	if width < 1 || width > 200000000 || height < 1 || height > 200000000/width {
		return LocalResourceEstimate{}, v2input.ErrInvalid
	}
	pixels := width * height
	return LocalResourceEstimate{PixelCount: pixels, RAMBytes: 400*pixels + 512*1024*1024,
		ScratchBytes: 64*pixels + 64*1024*1024, OutputBytes: 16*pixels + 32*1024*1024, VRAMBytes: 0}, nil
}
