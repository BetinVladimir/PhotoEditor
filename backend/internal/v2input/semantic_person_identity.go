package v2input

import (
	"encoding/json"
	"photoeditor/backend/internal/canonical"
)

// Own the operator-verified runtime pins before sealing the task. This only
// binds identities; the caller must independently verify qualification evidence.
func (f FrozenSemanticPersonTask) WithExecutionIdentity(model, runtime, device string) (FrozenSemanticPersonTask, error) {
	profile, err := bindSemanticModelExecutionIdentity(f.profile, f.closure.ParamsJCS, model, runtime, device)
	if err != nil {
		return FrozenSemanticPersonTask{}, err
	}
	f.profile = profile
	return f, nil
}

func bindSemanticModelExecutionIdentity(original string, paramsJCS []byte, model, runtime, device string) (string, error) {
	const zero = ""
	if !digestPattern.MatchString(model) || !digestPattern.MatchString(runtime) || !digestPattern.MatchString(device) {
		return zero, ErrInvalid
	}
	var params struct {
		Lock string `json:"model_lock_id"`
	}
	var profile map[string]any
	if json.Unmarshal(paramsJCS, &params) != nil || params.Lock != model || json.Unmarshal([]byte(original), &profile) != nil || (profile["backend"] != "cuda" && profile["backend"] != "mps") {
		return zero, ErrInvalid
	}
	for _, key := range []string{"model_lock_id", "model_lock_sha", "runtime_lock_sha", "device_profile_sha"} {
		if _, present := profile[key]; present {
			return zero, ErrConflict
		}
	}
	// V2InputIndex owns at least16MiB for the attempt lifetime, concurrently
	// with source mappings and output reservations. Legacy profile bytes stay
	// unchanged; version1 qualified profiles must include this runtime cost.
	resources, ok := profile["resource_estimate"].(map[string]any)
	if !ok {
		return zero, ErrInvalid
	}
	scratch, ok := resources["scratch_bytes"].(float64)
	if !ok || scratch < 0 || scratch > 9007199254740991-(16<<20) {
		return zero, ErrInvalid
	}
	resources["scratch_bytes"] = scratch + (16 << 20)
	profile["model_lock_id"], profile["model_lock_sha"] = model, model
	profile["runtime_lock_sha"], profile["device_profile_sha"] = runtime, device
	raw, err := canonical.Marshal(profile)
	if err != nil {
		return zero, err
	}
	return string(raw), nil
}
