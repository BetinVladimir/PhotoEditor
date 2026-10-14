package v2optical

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"sort"
)

type PerspectiveTaskPlan struct {
	LinesJCS                 string
	MaxPixels                int64
	PhysicalImageInputs      []v2input.Entry
	PhysicalImageOccurrences []PerspectiveImageOccurrence
	Operation                string
	Params                   []byte
	ParamsSHA                string
	Profile                  json.RawMessage
	DirectInputs             []v2input.Ref
	ReservedBytes            int64
}

// CompilePerspectiveTaskPlan owns canonical intent/profile and direct JSON roots.
// Original physical image closure, live SQL authorization, document publication
// and graph sealing remain mandatory admission steps outside this pure compiler.
func CompilePerspectiveTaskPlan(ctx context.Context, base v2input.SessionVersionBinding,
	paramsRaw, sourceRaw []byte, qualified VerifiedPerspectiveQualification,
	maxPixels int64, capacity PerspectiveResourceEstimate, lineOriginal ...[]byte) (PerspectiveTaskPlan, error) {
	var zero PerspectiveTaskPlan
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	params, err := ParsePerspectiveParams(paramsRaw)
	if err != nil || params.Base != base {
		return zero, v2input.ErrInvalid
	}
	pin, err := qualified.Pin()
	if err != nil {
		return zero, err
	}
	refs := []v2input.Ref{params.Base.Source, pin.Calibration, pin.Report}
	var linesJCS string
	if params.Mode == "auto" {
		if len(lineOriginal) != 1 {
			return zero, v2input.ErrInvalid
		}
		if _, err := VerifyPerspectiveLinesOriginal(params, lineOriginal[0]); err != nil {
			return zero, err
		}
		linesJCS = string(lineOriginal[0])
		refs = append(refs, *params.Lines)
	} else if len(lineOriginal) != 0 {
		return zero, v2input.ErrInvalid
	}
	seen := map[uuid.UUID]bool{}
	for _, ref := range refs {
		if seen[ref.ArtifactID] {
			return zero, v2input.ErrInvalid
		}
		seen[ref.ArtifactID] = true
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].ArtifactID.String() < refs[j].ArtifactID.String() })
	physical, occurrences, err := PerspectiveImageInputs(params, sourceRaw, pin.Report, pin.Calibration)
	if err != nil {
		return zero, err
	}
	var profile json.RawMessage
	if params.Mode == "auto" {
		profile, err = CompilePerspectiveAutoExecutionProfile(ctx, params, sourceRaw, []byte(linesJCS), qualified, maxPixels, capacity)
	} else {
		profile, err = CompilePerspectiveManualExecutionProfile(params, sourceRaw, qualified, maxPixels, capacity)
	}
	if err != nil {
		return zero, err
	}
	if params.Lines != nil {
		for _, entry := range physical {
			if entry.Ref.ArtifactID == params.Lines.ArtifactID {
				return zero, v2input.ErrInvalid
			}
		}
	}
	body, digest, err := params.Compile()
	if err != nil {
		return zero, err
	}
	var header struct {
		Resources PerspectiveResourceEstimate `json:"resource_estimate"`
	}
	if json.Unmarshal(profile, &header) != nil {
		return zero, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return PerspectiveTaskPlan{LinesJCS: linesJCS, MaxPixels: maxPixels, Operation: "optical.perspective", Params: body, ParamsSHA: digest, Profile: profile,
		DirectInputs: refs, PhysicalImageInputs: physical, PhysicalImageOccurrences: occurrences, ReservedBytes: header.Resources.OutputBytes}, nil
}
