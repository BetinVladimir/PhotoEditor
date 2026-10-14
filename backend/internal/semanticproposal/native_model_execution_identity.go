package semanticproposal

import (
	"encoding/json"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

func nativeModelProfileVersion(versions []int) (int, error) {
	if len(versions) > 1 {
		return 0, v2input.ErrInvalid
	}
	version := 1
	if len(versions) == 1 {
		version = versions[0]
	}
	if version != 0 && version != 1 {
		return 0, v2input.ErrInvalid
	}
	return version, nil
}
func bindNativeFaceExecutionIdentity(task nativeRegistrationTask, expected v2qualification.Expected) (nativeRegistrationTask, error) {
	switch f := task.(type) {
	case v2input.FrozenSemanticLandmarkTask:
		return f.WithExecutionIdentity(expected.ModelLockSHA, expected.RuntimeLockSHA, expected.DeviceProfileSHA)
	case v2input.FrozenSemanticFaceParseTask:
		return f.WithExecutionIdentity(expected.ModelLockSHA, expected.RuntimeLockSHA, expected.DeviceProfileSHA)
	default:
		return nil, v2input.ErrInvalid
	}
}
func validateNativeModelProfileCapacity(task v2input.DraftTask, ram, vram, scratch, output int64) error {
	var profile struct {
		Resources struct {
			RAM     int64 `json:"ram_bytes"`
			VRAM    int64 `json:"vram_bytes"`
			Scratch int64 `json:"scratch_bytes"`
			Output  int64 `json:"output_bytes"`
		} `json:"resource_estimate"`
	}
	if json.Unmarshal(task.ExecutionProfile, &profile) != nil || profile.Resources.RAM > ram || profile.Resources.VRAM > vram || profile.Resources.Scratch > scratch || profile.Resources.Output > output {
		return v2input.ErrInvalid
	}
	return nil
}
