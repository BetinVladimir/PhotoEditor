package v2render

import "photoeditor/backend/internal/v2input"

// LayerResources matches the CPU worker's conservative layer estimates. Counts
// refer to one compiled composition, including an optional protection leaf.
// Host scheduling must compare these values with available RAM/scratch quotas.
type LayerResourceEstimate struct {
	PixelCount   int64 `json:"pixel_count"`
	RAMBytes     int64 `json:"ram_bytes"`
	ScratchBytes int64 `json:"scratch_bytes"`
	OutputBytes  int64 `json:"output_bytes"`
	VRAMBytes    int64 `json:"vram_bytes"`
}

func LayerResources(width, height int64, maskRefs, maskNodes, tileSize int) (LayerResourceEstimate, error) {
	if width < 1 || height < 1 || width > 200000000 || height > 200000000/width || maskRefs < 1 || maskRefs > 257 || maskNodes < 1 || maskNodes > 256 || (tileSize != 256 && tileSize != 512 && tileSize != 1024) {
		return LayerResourceEstimate{}, v2input.ErrInvalid
	}
	pixels := width * height
	tile := min(width, int64(tileSize)) * min(height, int64(tileSize))
	mapped := (68 + 4*int64(maskRefs)) * pixels
	temporary := max(int64(256), 4*int64(maskNodes+3)) * tile
	return LayerResourceEstimate{PixelCount: pixels, RAMBytes: mapped + temporary + 256*1024*1024, ScratchBytes: mapped + 64*1024*1024, OutputBytes: 24*pixels + 32*1024*1024}, nil
}
