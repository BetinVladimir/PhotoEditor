package v2portrait

import (
	"bytes"
	"context"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// CompileRelightTaskPlan is a pure compiler. It does not publish artifacts,
// resolve producer authority, qualify a runtime or admit/activate a graph.
func CompileRelightTaskPlan(ctx context.Context, binding v2input.SessionVersionBinding,
	sourceRaw, paramsRaw []byte, runtimeSHA string, capacity LocalResourceEstimate) (LocalTaskPlan, error) {
	var zero LocalTaskPlan
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if len(paramsRaw) < 2 || len(paramsRaw) > 65536 {
		return zero, v2input.ErrInvalid
	}
	var params RelightParams
	if json.Unmarshal(paramsRaw, &params) != nil || params.Base != binding {
		return zero, v2input.ErrInvalid
	}
	body, digest, err := params.Compile()
	normalized, canonicalErr := canonical.Bytes(paramsRaw)
	if err != nil || canonicalErr != nil || !bytes.Equal(body, normalized) {
		return zero, v2input.ErrInvalid
	}
	profile, err := CompileRelightExecutionProfile(params, sourceRaw, runtimeSHA, capacity)
	if err != nil {
		return zero, err
	}
	var header struct {
		Resources LocalResourceEstimate `json:"resource_estimate"`
	}
	if json.Unmarshal(profile, &header) != nil {
		return zero, v2input.ErrInvalid
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	return LocalTaskPlan{Operation: "image.relight", Params: body, ParamsSHA: digest, Profile: profile, ReservedBytes: header.Resources.OutputBytes}, nil
}
