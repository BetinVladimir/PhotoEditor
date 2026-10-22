package semanticreview

// CompileRetouchPreviewResources admits the independent preview compute path,
// including full-face BeautyFilter ROI, owned arrays and four PNG files,
// source/mask/protection scratch and WorkingImage/evidence/presentation JSON.
// Runtime overhead and index costs must be measured by the installed owner.
func CompileRetouchPreviewResources(width, height int64, owner ExposureOwnerCosts,
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
	// Covers worker compute floor 512 MiB + 128*pixels + 256*4194304,
	// plus simultaneous verified input/presentation/staging owners and LCMS workspace.
	compute := 960*pixels + 768*1024*1024 + 256*4194304
	// Largest PNG includes an ICC profile of at most 1 MiB.
	// All face corrections coexist until compositing: one RGB delta and one
	// float32 weight plane per face, with two NPY headers per reservation.
	roiPixels := pixels
	if roiPixels > 4194304 {
		roiPixels = 4194304
	}
	faceScratch := 64 * (16*roiPixels + 2*8192)
	scratch := 64*pixels + faceScratch + 64*1024*1024 + 4*16384 + 5*displayPixels + 1048576 + 65536
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
