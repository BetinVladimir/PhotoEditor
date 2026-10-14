package v2portrait

import "photoeditor/backend/internal/v2input"

// RelightAnalysisMinimumResources accounts for frozen source occurrence
// materialization, bounded input-index scratch and full-size processing buffers.
// Deployment qualification may require larger reserves; this is a lower bound.
func RelightAnalysisMinimumResources(request RelightAnalysisPlanRequest) (LocalResourceEstimate, error) {
	specs, err := AnalysisSourceArrays(request)
	if err != nil {
		return LocalResourceEstimate{}, err
	}
	if request.Operation != "depth.predict" && request.Operation != "mask.person" {
		return LocalResourceEstimate{}, v2input.ErrInvalid
	}
	pixels := specs[0].Width * specs[0].Height
	ram := 128*pixels + (512 << 20)
	if ram < 1<<30 {
		ram = 1 << 30
	}
	scratch := int64(32<<20) + 16*pixels
	for _, spec := range specs {
		components := int64(spec.Channels)
		if spec.Alpha {
			components = 1
		}
		scratch += spec.Width*spec.Height*components*4 + 65536
	}
	return LocalResourceEstimate{PixelCount: pixels, RAMBytes: ram, ScratchBytes: scratch, OutputBytes: 8*pixels + 65536}, nil
}
