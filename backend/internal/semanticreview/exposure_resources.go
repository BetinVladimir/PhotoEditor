package semanticreview

// ExposureOwnerCosts must be measured by the installed runtime/closure builder.
// They exclude image arrays already counted by EncodedLocalExposureMemory.
type ExposureOwnerCosts struct {
	RuntimeRAMBytes        int64 `json:"runtime_ram_bytes"`
	InputIndexScratchBytes int64 `json:"input_index_scratch_bytes"`
}

type ExposureResourceEstimate struct {
	RAMBytes     int64 `json:"ram_bytes"`
	VRAMBytes    int64 `json:"vram_bytes"`
	ScratchBytes int64 `json:"scratch_bytes"`
	OutputBytes  int64 `json:"output_bytes"`
	PixelCount   int64 `json:"pixel_count"`
}

// CompileExposureResources covers compute, independent quality replay, staged
// NPY plus three JSON outputs and simultaneously mapped source/mask/protection.
// This is a resource calculation, not model/runtime qualification authority.
func CompileExposureResources(width, height int64, owner ExposureOwnerCosts, limits ExposureResourceEstimate) (ExposureResourceEstimate, error) {
	var empty ExposureResourceEstimate
	compute, err := EncodedLocalExposureMemory(width, height)
	const safe int64 = 9007199254740991
	if err != nil || owner.RuntimeRAMBytes < 1 || owner.RuntimeRAMBytes > safe-compute || owner.InputIndexScratchBytes < 16*1024*1024 || owner.InputIndexScratchBytes > safe {
		return empty, ErrInvalid
	}
	pixels := width * height
	mapped := 20*pixels + 3*16384
	if owner.InputIndexScratchBytes > safe-mapped {
		return empty, ErrInvalid
	}
	cost := ExposureResourceEstimate{RAMBytes: compute + owner.RuntimeRAMBytes, ScratchBytes: mapped + owner.InputIndexScratchBytes, OutputBytes: 12*pixels + 16384 + 3*1048576, PixelCount: pixels}
	for _, value := range []int64{limits.RAMBytes, limits.VRAMBytes, limits.ScratchBytes, limits.OutputBytes, limits.PixelCount} {
		if value < 0 || value > safe {
			return empty, ErrInvalid
		}
	}
	if limits.RAMBytes < cost.RAMBytes || limits.ScratchBytes < cost.ScratchBytes || limits.OutputBytes < cost.OutputBytes || limits.PixelCount < pixels {
		return empty, ErrInvalid
	}
	return cost, nil
}
