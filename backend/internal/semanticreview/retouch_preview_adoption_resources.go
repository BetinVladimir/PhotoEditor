package semanticreview

// CompileShownRetouchAdoptionResources uses the shared exact-byte copy budget.
// No model inference or device allocation occurs in the adoption task.
func CompileShownRetouchAdoptionResources(width, height int64, owner ExposureOwnerCosts, limits ExposureResourceEstimate) (ExposureResourceEstimate, error) {
	if limits.VRAMBytes != 0 {
		return ExposureResourceEstimate{}, ErrInvalid
	}
	return CompileShownExposureAdoptionResources(width, height, owner, limits)
}
