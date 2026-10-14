package v2optical

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"sort"
)

type NegativeTaskPlan struct {
	PhysicalImageInputs      []v2input.Entry
	PhysicalImageOccurrences []NegativeImageOccurrence
	Operation                string
	Params                   []byte
	ParamsSHA                string
	Profile                  json.RawMessage
	DirectInputs             []v2input.Ref
	ReservedBytes            int64
}

// CompileNegativeTaskPlan owns canonical intent/profile and direct JSON roots.
// Original physical image closure, live SQL authorization, document publication
// and graph sealing remain mandatory admission steps outside this pure compiler.
func CompileNegativeTaskPlan(ctx context.Context, base v2input.SessionVersionBinding,
	paramsRaw, sourceRaw []byte, qualified VerifiedNegativeQualification,
	capacity NegativeResourceEstimate) (NegativeTaskPlan, error) {
	var zero NegativeTaskPlan
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	params, err := ParseNegativeParams(paramsRaw)
	if err != nil || params.Base != base {
		return zero, v2input.ErrInvalid
	}
	pin, err := qualified.Pin()
	if err != nil {
		return zero, err
	}
	refs := []v2input.Ref{params.Base.Source, params.Calibration, pin.Report}
	seen := map[uuid.UUID]bool{}
	for _, ref := range refs {
		if seen[ref.ArtifactID] {
			return zero, v2input.ErrInvalid
		}
		seen[ref.ArtifactID] = true
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].ArtifactID.String() < refs[j].ArtifactID.String() })
	physical, occurrences, err := NegativeImageInputs(params, sourceRaw, pin.Report)
	if err != nil {
		return zero, err
	}
	profile, err := CompileNegativeExecutionProfile(params, sourceRaw, qualified, capacity)
	if err != nil {
		return zero, err
	}
	body, digest, err := params.Compile()
	if err != nil {
		return zero, err
	}
	var header struct {
		Resources NegativeResourceEstimate `json:"resource_estimate"`
	}
	if json.Unmarshal(profile, &header) != nil {
		return zero, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return NegativeTaskPlan{Operation: "optical.negative", Params: body, ParamsSHA: digest, Profile: profile,
		DirectInputs: refs, PhysicalImageInputs: physical, PhysicalImageOccurrences: occurrences, ReservedBytes: header.Resources.OutputBytes}, nil
}
