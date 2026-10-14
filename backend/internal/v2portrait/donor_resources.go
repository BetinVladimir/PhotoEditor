package v2portrait

import "photoeditor/backend/internal/v2input"

// DonorResourceEstimate matches the worker's conservative CPU donor budget.
// Actual capacity, sealed profile and target qualification remain required.
type DonorResourceEstimate struct {
	SourcePixelCount     int64 `json:"source_pixel_count"`
	DonorPixelCount      int64 `json:"donor_pixel_count"`
	PatchPixelCount      int64 `json:"patch_pixel_count"`
	RAMBytes             int64 `json:"ram_bytes"`
	MaskScratchBytes     int64 `json:"mask_scratch_bytes"`
	MaterialScratchBytes int64 `json:"material_scratch_bytes"`
	OutputScratchBytes   int64 `json:"output_scratch_bytes"`
	ScratchBytes         int64 `json:"scratch_bytes"`
	OutputBytes          int64 `json:"output_bytes"`
	VRAMBytes            int64 `json:"vram_bytes"`
}

func DonorPortraitResources(sw, sh, dw, dh, pw, ph int64) (DonorResourceEstimate, error) {
	valid := func(w, h, max int64) bool { return w > 0 && h > 0 && w <= max && h <= max/w }
	if !valid(sw, sh, 200000000) || !valid(dw, dh, 200000000) ||
		!valid(pw, ph, 1048576) || pw > sw || ph > sh {
		return DonorResourceEstimate{}, v2input.ErrInvalid
	}
	s, d, p := sw*sh, dw*dh, pw*ph
	mask, material, output := 8*s+8192, 20*(s+d)+24576, 16*s+32768
	return DonorResourceEstimate{SourcePixelCount: s, DonorPixelCount: d, PatchPixelCount: p,
		RAMBytes: 400*(s+d+p) + 512*1024*1024, MaskScratchBytes: mask,
		MaterialScratchBytes: material, OutputScratchBytes: output,
		ScratchBytes: mask + material + output + 64*1024*1024,
		OutputBytes:  16*s + 32*1024*1024}, nil
}
