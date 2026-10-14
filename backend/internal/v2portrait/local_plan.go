package v2portrait

import (
	"bytes"
	"context"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type LocalTaskPlan struct {
	Operation     string
	Params        json.RawMessage
	ParamsSHA     string
	Profile       json.RawMessage
	ReservedBytes int64
}

// CompileLocalTaskPlan binds the operation-specific Params and CPU profile.
// It performs no publication, graph admission or artifact authorization.
func CompileLocalTaskPlan(ctx context.Context, operation string, binding v2input.SessionVersionBinding,
	sourceRaw, paramsRaw []byte, runtimeSHA string, capacity LocalResourceEstimate) (LocalTaskPlan, error) {
	var zero LocalTaskPlan
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if len(paramsRaw) < 2 || len(paramsRaw) > 65536 {
		return zero, v2input.ErrInvalid
	}
	var body []byte
	var digest string
	var base v2input.SessionVersionBinding
	var mask, guard v2input.Ref
	var err error
	switch operation {
	case "portrait.hair":
		var p HairParams
		if json.Unmarshal(paramsRaw, &p) != nil {
			return zero, v2input.ErrInvalid
		}
		body, digest, err = p.Compile()
		base, mask, guard = p.Base, p.Mask, p.Protected
	case "portrait.clothing":
		var p ClothingParams
		if json.Unmarshal(paramsRaw, &p) != nil {
			return zero, v2input.ErrInvalid
		}
		body, digest, err = p.Compile()
		base, mask, guard = p.Base, p.Mask, p.Protected
	default:
		return zero, v2input.ErrInvalid
	}
	normalized, e := canonical.Bytes(paramsRaw)
	if err != nil || e != nil || !bytes.Equal(body, normalized) || base != binding ||
		mask.ArtifactID == guard.ArtifactID || mask.ArtifactID == base.Source.ArtifactID || guard.ArtifactID == base.Source.ArtifactID {
		return zero, v2input.ErrInvalid
	}
	profile, err := CompileLocalExecutionProfile(binding, sourceRaw, runtimeSHA, capacity)
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
	return LocalTaskPlan{Operation: operation, Params: body, ParamsSHA: digest, Profile: profile, ReservedBytes: header.Resources.OutputBytes}, nil
}
