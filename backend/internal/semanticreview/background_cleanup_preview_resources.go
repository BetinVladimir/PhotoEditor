package semanticreview

// CompileBackgroundCleanupPreviewResources uses the same numerical/display
// algorithm as removal. This shared accounting grants no qualification authority.
func CompileBackgroundCleanupPreviewResources(width, height int64, owner ExposureOwnerCosts,
	limits ExposureResourceEstimate, backend string, measuredDeviceBytes int64) (ExposureResourceEstimate, error) {
	return CompileRemovalPreviewResources(width, height, owner, limits, backend, measuredDeviceBytes)
}
