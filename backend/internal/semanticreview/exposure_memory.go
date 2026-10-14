package semanticreview

// LocalExposureMemory matches the worker's local_exposure_memory kernel cost.
// Trusted loaders must add decoding, model and other retained owner costs.
// This cost alone does not constitute a complete execution profile.
func LocalExposureMemory(width, height int64) (int64, error) {
	if width < 1 || height < 1 || width > 200000000/height {
		return 0, ErrInvalid
	}
	return width * height * 96, nil
}

// LocalExposureExecutionMemory combines qualified loader peak and retained
// owner costs with kernel cost. The loader must supply both estimates; retained
// excludes RGB/mask/protection already accounted for by LocalExposureMemory.
func LocalExposureExecutionMemory(width, height, loaderPeak, retainedLoader int64) (int64, error) {
	kernel, err := LocalExposureMemory(width, height)
	const safe = int64(9007199254740991)
	if err != nil || loaderPeak < 0 || retainedLoader < 0 || retainedLoader > loaderPeak || loaderPeak > safe || retainedLoader > safe-kernel {
		return 0, ErrInvalid
	}
	required := retainedLoader + kernel
	if loaderPeak > required {
		required = loaderPeak
	}
	return required, nil
}

// EncodedLocalExposureMemory matches the complete worker gamma24 computation
// path, including retained loader RGB and document overhead. Output publication
// and shared runtime costs must be admitted separately by the installed adapter.
func EncodedLocalExposureMemory(width, height int64) (int64, error) {
	kernel, err := LocalExposureMemory(width, height)
	if err != nil {
		return 0, err
	}
	return kernel/96*204 + 64*1024*1024, nil
}
