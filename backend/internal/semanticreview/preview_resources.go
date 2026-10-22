package semanticreview

// CompileExposurePreviewResources admits the independent preview compute path,
// including exact numerical replay, owned presentation arrays, four PNG files,
// source/mask/protection scratch and WorkingImage/evidence/presentation JSON.
// Runtime overhead and index costs must be measured by the installed owner.
func CompileExposurePreviewResources(width, height int64, owner ExposureOwnerCosts,
	limits ExposureResourceEstimate) (ExposureResourceEstimate, error) {
	const safe int64 = 9007199254740991
	empty := ExposureResourceEstimate{}
	if width < 1 || height < 1 || width > 200000000 || height > 200000000/width ||
		owner.RuntimeRAMBytes < 1 || owner.InputIndexScratchBytes < 16*1024*1024 {
		return empty, ErrInvalid
	}
	pixels := width * height
	// A square bound avoids disagreement about rounded aspect ratios across runtimes.
	displayPixels := pixels
	if displayPixels > 2560*2560 {
		displayPixels = 2560 * 2560
	}
	compute := 640*pixels + 128*1024*1024
	// Largest PNG includes an ICC profile of at most 1 MiB.
	scratch := 32*pixels + 4*16384 + 5*displayPixels + 1048576 + 65536
	if owner.RuntimeRAMBytes > safe-compute || owner.InputIndexScratchBytes > safe-scratch {
		return empty, ErrInvalid
	}
	cost := ExposureResourceEstimate{RAMBytes: compute + owner.RuntimeRAMBytes,
		ScratchBytes: scratch + owner.InputIndexScratchBytes, OutputBytes: 12*pixels + 16384 + 20*displayPixels + 4*65536 + 8*1048576, PixelCount: pixels}
	for _, value := range []int64{limits.RAMBytes, limits.VRAMBytes, limits.ScratchBytes, limits.OutputBytes, limits.PixelCount} {
		if value < 0 || value > safe {
			return empty, ErrInvalid
		}
	}
	if limits.RAMBytes < cost.RAMBytes || limits.ScratchBytes < cost.ScratchBytes ||
		limits.OutputBytes < cost.OutputBytes || limits.PixelCount < pixels {
		return empty, ErrInvalid
	}
	return cost, nil
}
