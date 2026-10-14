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

type MultiTaskPlan struct {
	Operation                string
	Params                   []byte
	ParamsSHA                string
	Profile                  json.RawMessage
	DirectInputs             []v2input.Ref
	PhysicalImageInputs      []v2input.Entry
	PhysicalImageOccurrences []MultiImageOccurrence
	ReservedBytes            int64
}

// CompileMultiTaskPlan is a content compiler, not a live admission capability.
func CompileMultiTaskPlan(ctx context.Context, operation string, paramsRaw []byte, originals [][]byte,
	qualified VerifiedMultiQualification, capacity MultiResourceEstimate) (MultiTaskPlan, error) {
	var zero MultiTaskPlan
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	pin, err := qualified.Pin()
	if err != nil {
		return zero, err
	}
	profile, err := CompileMultiExecutionProfile(operation, paramsRaw, originals, qualified, capacity)
	if err != nil {
		return zero, err
	}
	physical, occurrences, err := MultiImageInputs(operation, paramsRaw, originals, pin.Report)
	if err != nil {
		return zero, err
	}
	var frames []v2input.SessionVersionBinding
	var body []byte
	var digest string
	if operation == "optical.panorama" {
		p, e := ParsePanoramaParams(paramsRaw)
		if e != nil {
			return zero, e
		}
		frames = p.Frames
		body, digest, err = p.Compile()
	} else {
		p, e := ParseFocusStackParams(paramsRaw)
		if e != nil {
			return zero, e
		}
		frames = p.Frames
		body, digest, err = p.Compile()
	}
	if err != nil {
		return zero, err
	}
	refs := []v2input.Ref{pin.Calibration, pin.Report}
	for _, frame := range frames {
		refs = append(refs, frame.Source)
	}
	unique := map[uuid.UUID]v2input.Ref{}
	for _, ref := range refs {
		if old, ok := unique[ref.ArtifactID]; ok && old != ref {
			return zero, v2input.ErrInvalid
		}
		unique[ref.ArtifactID] = ref
	}
	refs = nil
	for _, ref := range unique {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].ArtifactID.String() < refs[j].ArtifactID.String() })
	var header struct {
		Resources MultiResourceEstimate `json:"resource_estimate"`
	}
	if json.Unmarshal(profile, &header) != nil {
		return zero, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return MultiTaskPlan{Operation: operation, Params: body, ParamsSHA: digest, Profile: profile, DirectInputs: refs,
		PhysicalImageInputs: physical, PhysicalImageOccurrences: occurrences, ReservedBytes: header.Resources.OutputBytes}, nil
}
func MultiTaskInputEntries(ctx context.Context, plan MultiTaskPlan, originals [][]byte,
	qualified VerifiedMultiQualification, publishedParams v2input.Ref) ([]v2input.Entry, []v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if !validPerspectiveRef(publishedParams) || publishedParams.SHA256 != plan.ParamsSHA || len(plan.Profile) > 65536 {
		return nil, nil, v2input.ErrInvalid
	}
	var header struct {
		Resources MultiResourceEstimate `json:"resource_estimate"`
	}
	if json.Unmarshal(plan.Profile, &header) != nil {
		return nil, nil, v2input.ErrInvalid
	}
	rebuilt, err := CompileMultiTaskPlan(ctx, plan.Operation, plan.Params, originals, qualified, header.Resources)
	if err != nil {
		return nil, nil, err
	}
	if !reflect.DeepEqual(plan, rebuilt) || !bytes.Equal(plan.Profile, rebuilt.Profile) {
		return nil, nil, v2input.ErrInvalid
	}
	pin, err := qualified.Pin()
	if err != nil {
		return nil, nil, err
	}
	entries := []v2input.Entry{{Ref: publishedParams, Contract: "Params"}, {Ref: pin.Calibration, Contract: "Calibration"}, {Ref: pin.Report, Contract: "Report"}}
	for _, ref := range rebuilt.DirectInputs {
		if ref == pin.Calibration || ref == pin.Report {
			continue
		}
		entries = append(entries, v2input.Entry{Ref: ref, Contract: "WorkingImage"})
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
