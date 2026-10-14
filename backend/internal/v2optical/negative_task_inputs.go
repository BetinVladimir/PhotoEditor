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

// NegativeTaskInputEntries reconstructs an exact immutable transport inventory
// from qualified originals. Live relational/physical admission remains required.
func NegativeTaskInputEntries(ctx context.Context, plan NegativeTaskPlan, sourceRaw []byte, qualified VerifiedNegativeQualification, publishedParams v2input.Ref) ([]v2input.Entry, []v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if publishedParams.ArtifactID == uuid.Nil || publishedParams.SHA256 != plan.ParamsSHA {
		return nil, nil, v2input.ErrInvalid
	}
	params, err := ParseNegativeParams(plan.Params)
	if err != nil {
		return nil, nil, err
	}
	var profile struct {
		Resources NegativeResourceEstimate `json:"resource_estimate"`
	}
	if len(plan.Profile) > 65536 || json.Unmarshal(plan.Profile, &profile) != nil {
		return nil, nil, v2input.ErrInvalid
	}
	rebuilt, err := CompileNegativeTaskPlan(ctx, params.Base, plan.Params, sourceRaw, qualified, profile.Resources)
	if err != nil {
		return nil, nil, err
	}
	if plan.Operation != rebuilt.Operation || plan.ParamsSHA != rebuilt.ParamsSHA || plan.ReservedBytes != rebuilt.ReservedBytes || !bytes.Equal(plan.Params, rebuilt.Params) || !bytes.Equal(plan.Profile, rebuilt.Profile) || !reflect.DeepEqual(plan.DirectInputs, rebuilt.DirectInputs) || !reflect.DeepEqual(plan.PhysicalImageInputs, rebuilt.PhysicalImageInputs) || !reflect.DeepEqual(plan.PhysicalImageOccurrences, rebuilt.PhysicalImageOccurrences) {
		return nil, nil, v2input.ErrInvalid
	}
	pin, err := qualified.Pin()
	if err != nil {
		return nil, nil, err
	}
	entries := []v2input.Entry{{Ref: publishedParams, Contract: "Params"}, {Ref: params.Base.Source, Contract: "WorkingImage"}, {Ref: params.Calibration, Contract: "NegativeCalibration"}, {Ref: pin.Report, Contract: "QualificationReport"}}
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
