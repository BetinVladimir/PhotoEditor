package semanticreview

// CompileShownExposureAdoptionResources covers version-pinned NPY input and
// staged byte copy concurrently, bounded tile validation, JSON and runtime/index
// overhead. No exposure replay, display conversion or device allocation occurs.
func CompileShownExposureAdoptionResources(width, height int64, owner ExposureOwnerCosts,
	limits ExposureResourceEstimate) (ExposureResourceEstimate, error) {
	const safe int64 = 9007199254740991
	empty := ExposureResourceEstimate{}
	if width < 1 || height < 1 || width > 200000000 || height > 200000000/width ||
		owner.RuntimeRAMBytes < 1 || owner.InputIndexScratchBytes < 16*1024*1024 {
		return empty, ErrInvalid
	}
	pixels := width * height
	ram := int64(64 * 1024 * 1024)
	scratch := 24*pixels + 2*16384
	if owner.RuntimeRAMBytes > safe-ram || owner.InputIndexScratchBytes > safe-scratch {
		return empty, ErrInvalid
	}
	cost := ExposureResourceEstimate{RAMBytes: ram + owner.RuntimeRAMBytes, ScratchBytes: scratch + owner.InputIndexScratchBytes,
		OutputBytes: 12*pixels + 16384 + 1048576, PixelCount: pixels}
	for _, v := range []int64{limits.RAMBytes, limits.VRAMBytes, limits.ScratchBytes, limits.OutputBytes, limits.PixelCount} {
		if v < 0 || v > safe {
			return empty, ErrInvalid
		}
	}
	if limits.RAMBytes < cost.RAMBytes || limits.ScratchBytes < cost.ScratchBytes || limits.OutputBytes < cost.OutputBytes || limits.PixelCount < pixels {
		return empty, ErrInvalid
	}
	return cost, nil
}
