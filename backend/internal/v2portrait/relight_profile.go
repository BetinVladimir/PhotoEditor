package v2portrait

import (
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// CompileRelightExecutionProfile binds canonical source bytes and the selected
// runtime to worker reservations. The caller must independently establish
// qualification, producer authority, live quotas and host capacity.
func CompileRelightExecutionProfile(params RelightParams, sourceRaw []byte,
	runtimeSHA string, capacity LocalResourceEstimate) (json.RawMessage, error) {
	if _, _, err := params.Compile(); err != nil {
		return nil, err
	}
	if _, err := domain.ParseDigest(runtimeSHA); err != nil {
		return nil, v2input.ErrInvalid
	}
	source, err := VerifyDonorWorkingImage(params.Base, sourceRaw)
	if err != nil {
		return nil, err
	}
	resources, err := LocalPortraitResources(source.Canvas.Width, source.Canvas.Height)
	if err != nil {
		return nil, err
	}
	have := []int64{capacity.PixelCount, capacity.RAMBytes, capacity.ScratchBytes, capacity.OutputBytes, capacity.VRAMBytes}
	need := []int64{resources.PixelCount, resources.RAMBytes, resources.ScratchBytes, resources.OutputBytes, resources.VRAMBytes}
	for i, value := range have {
		if value < need[i] || value > 9007199254740991 {
			return nil, v2input.ErrInvalid
		}
	}
	return canonical.Marshal(struct {
		Backend    string                `json:"backend"`
		Renderer   string                `json:"renderer_version"`
		RuntimeSHA string                `json:"relight_runtime_sha"`
		Resources  LocalResourceEstimate `json:"resource_estimate"`
	}{"cpu", "v2.2", runtimeSHA, resources})
}
