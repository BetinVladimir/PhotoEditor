package v2input

// Bind exact operator-verified model/runtime/device pins without changing Params
// or the declared closure. Qualification and capacity checks are caller duties.
func (f FrozenSemanticDetectorTask) WithExecutionIdentity(model, runtime, device string) (FrozenSemanticDetectorTask, error) {
	profile, err := bindSemanticModelExecutionIdentity(f.profile, f.closure.ParamsJCS, model, runtime, device)
	if err != nil {
		return FrozenSemanticDetectorTask{}, err
	}
	f.profile = profile
	return f, nil
}

func (f FrozenSemanticLandmarkTask) WithExecutionIdentity(model, runtime, device string) (FrozenSemanticLandmarkTask, error) {
	profile, err := bindSemanticModelExecutionIdentity(f.profile, f.closure.ParamsJCS, model, runtime, device)
	if err != nil {
		return FrozenSemanticLandmarkTask{}, err
	}
	f.profile = profile
	return f, nil
}

func (f FrozenSemanticFaceParseTask) WithExecutionIdentity(model, runtime, device string) (FrozenSemanticFaceParseTask, error) {
	profile, err := bindSemanticModelExecutionIdentity(f.profile, f.closure.ParamsJCS, model, runtime, device)
	if err != nil {
		return FrozenSemanticFaceParseTask{}, err
	}
	f.profile = profile
	return f, nil
}
