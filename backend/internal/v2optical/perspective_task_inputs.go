package v2optical

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"reflect"
	"sort"
)

// PerspectiveTaskInputEntries reconstructs an exact immutable transport inventory
// from qualified originals. Live relational/physical admission remains required.
func PerspectiveTaskInputEntries(ctx context.Context, plan PerspectiveTaskPlan, sourceRaw []byte, qualified VerifiedPerspectiveQualification, publishedParams v2input.Ref) ([]v2input.Entry, []v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if publishedParams.ArtifactID == uuid.Nil || publishedParams.SHA256 != plan.ParamsSHA {
		return nil, nil, v2input.ErrInvalid
	}
	params, err := ParsePerspectiveParams(plan.Params)
	if err != nil {
		return nil, nil, err
	}
	var profile struct {
		Resources PerspectiveResourceEstimate `json:"resource_estimate"`
	}
	if len(plan.Profile) > 65536 || json.Unmarshal(plan.Profile, &profile) != nil {
		return nil, nil, v2input.ErrInvalid
	}
	var originals [][]byte
	if params.Mode == "auto" {
		originals = [][]byte{[]byte(plan.LinesJCS)}
	} else if plan.LinesJCS != "" {
		return nil, nil, v2input.ErrInvalid
	}
	rebuilt, err := CompilePerspectiveTaskPlan(ctx, params.Base, plan.Params, sourceRaw, qualified, plan.MaxPixels, profile.Resources, originals...)
	if err != nil {
		return nil, nil, err
	}
	if plan.LinesJCS != rebuilt.LinesJCS || plan.MaxPixels != rebuilt.MaxPixels || plan.Operation != rebuilt.Operation || plan.ParamsSHA != rebuilt.ParamsSHA || plan.ReservedBytes != rebuilt.ReservedBytes || !bytes.Equal(plan.Params, rebuilt.Params) || !bytes.Equal(plan.Profile, rebuilt.Profile) || !reflect.DeepEqual(plan.DirectInputs, rebuilt.DirectInputs) || !reflect.DeepEqual(plan.PhysicalImageInputs, rebuilt.PhysicalImageInputs) || !reflect.DeepEqual(plan.PhysicalImageOccurrences, rebuilt.PhysicalImageOccurrences) {
		return nil, nil, v2input.ErrInvalid
	}
	pin, err := qualified.Pin()
	if err != nil {
		return nil, nil, err
	}
	entries := []v2input.Entry{{Ref: publishedParams, Contract: "Params"}, {Ref: params.Base.Source, Contract: "WorkingImage"}, {Ref: pin.Calibration, Contract: "Calibration"}, {Ref: pin.Report, Contract: "Report"}}
	if params.Lines != nil {
		entries = append(entries, v2input.Entry{Ref: *params.Lines, Contract: "LineSegments"})
	}
	entries = append(entries, rebuilt.PhysicalImageInputs...)
	seen := map[uuid.UUID]bool{}
	for _, entry := range entries {
		if seen[entry.Ref.ArtifactID] {
			return nil, nil, v2input.ErrInvalid
		}
		seen[entry.Ref.ArtifactID] = true
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Ref.ArtifactID.String() < entries[j].Ref.ArtifactID.String() })
	direct := append([]v2input.Ref{publishedParams}, rebuilt.DirectInputs...)
	sort.Slice(direct, func(i, j int) bool { return direct[i].ArtifactID.String() < direct[j].ArtifactID.String() })
	return entries, direct, ctx.Err()
}
