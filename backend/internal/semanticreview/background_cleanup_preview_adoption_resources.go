package semanticreview

// CompileShownBackgroundCleanupAdoptionResources uses the shared exact-byte copy budget.
// No model inference or device allocation occurs in the adoption task.
func CompileShownBackgroundCleanupAdoptionResources(width, height int64, owner ExposureOwnerCosts, limits ExposureResourceEstimate) (ExposureResourceEstimate, error) {
	return CompileShownExposureAdoptionResources(width, height, owner, limits)
}
