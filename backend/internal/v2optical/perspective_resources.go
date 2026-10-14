package v2optical

import "photoeditor/backend/internal/v2input"

// PerspectiveResourceEstimate mirrors the CPU worker, without qualification authority.
type PerspectiveResourceEstimate struct {
	PixelCount   int64 `json:"pixel_count"`
	VRAMBytes    int64 `json:"vram_bytes"`
	RAMBytes     int64 `json:"ram_bytes"`
	ScratchBytes int64 `json:"scratch_bytes"`
	OutputBytes  int64 `json:"output_bytes"`
}

func PerspectiveResources(sourceWidth, sourceHeight, outputWidth, outputHeight int64) (PerspectiveResourceEstimate, error) {
	if sourceWidth < 1 || sourceHeight < 1 || outputWidth < 1 || outputHeight < 1 || sourceWidth > 200000000 || outputWidth > 200000000 || sourceHeight > 200000000/sourceWidth || outputHeight > 200000000/outputWidth {
		return PerspectiveResourceEstimate{}, v2input.ErrInvalid
	}
	source, output := sourceWidth*sourceHeight, outputWidth*outputHeight
	return PerspectiveResourceEstimate{PixelCount: output, RAMBytes: 32*source + 20*output + 512*1024*1024,
		ScratchBytes: 32*source + 20*output + 64*1024*1024, OutputBytes: 20*output + 32*1024*1024}, nil
}

// ValidatePerspectiveCapacity rejects malformed and under-reserved deployment bounds.
func ValidatePerspectiveCapacity(required, capacity PerspectiveResourceEstimate) error {
	const safe = int64(1<<53 - 1)
	if capacity.PixelCount != required.PixelCount || capacity.VRAMBytes != 0 {
		return v2input.ErrInvalid
	}
	for _, value := range []int64{capacity.PixelCount, capacity.RAMBytes, capacity.ScratchBytes, capacity.OutputBytes} {
		if value < 0 || value > safe {
			return v2input.ErrInvalid
		}
	}
	if capacity.RAMBytes < required.RAMBytes || capacity.ScratchBytes < required.ScratchBytes || capacity.OutputBytes < required.OutputBytes {
		return v2input.ErrInvalid
	}
	return nil
}
