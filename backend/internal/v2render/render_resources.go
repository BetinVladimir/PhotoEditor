package v2render

import "photoeditor/backend/internal/v2input"

type RenderProviderPeak struct {
	RAMBytes     int64 `json:"ram_bytes"`
	ScratchBytes int64 `json:"scratch_bytes"`
	VRAMBytes    int64 `json:"vram_bytes"`
	OutputBytes  int64 `json:"output_bytes"`
}

// RenderResources mirrors worker same-canvas accounting. Provider costs must
// come from a qualified runtime; this calculation does not qualify a renderer.
func RenderResources(width, height int64, maskRefs, maskNodes, tileSize int, alphaMode string, provider RenderProviderPeak) (LayerResourceEstimate, error) {
	const safe int64 = 9007199254740991
	invalid := func() (LayerResourceEstimate, error) { return LayerResourceEstimate{}, v2input.ErrInvalid }
	if width < 1 || height < 1 || width > 200000000 || height > 200000000/width || maskRefs < 0 || maskRefs > 257 || maskNodes < 0 || maskNodes > 512 || (tileSize != 256 && tileSize != 512 && tileSize != 1024) || (alphaMode != "require_float32" && alphaMode != "normalize_uint16") {
		return invalid()
	}
	for _, value := range []int64{provider.RAMBytes, provider.ScratchBytes, provider.VRAMBytes, provider.OutputBytes} {
		if value < 0 || value > safe {
			return invalid()
		}
	}
	pixels := width * height
	tile := min(width, int64(tileSize)) * min(height, int64(tileSize))
	mapped := (112+4*int64(maskRefs))*pixels + 8192*int64(16+maskRefs)
	if alphaMode == "normalize_uint16" {
		mapped += 4*pixels + 8192
	}
	temporary := max(int64(256), 4*int64(maskNodes+3)) * tile
	result := LayerResourceEstimate{PixelCount: pixels, RAMBytes: mapped + temporary + 256*1024*1024 + provider.RAMBytes, ScratchBytes: mapped + 64*1024*1024 + provider.ScratchBytes, OutputBytes: 24*pixels + 32*1024*1024 + provider.OutputBytes, VRAMBytes: provider.VRAMBytes}
	for _, value := range []int64{result.RAMBytes, result.ScratchBytes, result.OutputBytes, result.VRAMBytes} {
		if value > safe {
			return invalid()
		}
	}
	return result, nil
}
