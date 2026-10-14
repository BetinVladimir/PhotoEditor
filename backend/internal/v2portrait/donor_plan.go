package v2portrait

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"sort"
)

type DonorTaskPlan struct {
	LocalTaskPlan
	Inputs                   []v2input.Ref
	PhysicalImageInputs      []v2input.Entry
	PhysicalImageOccurrences []DonorImageOccurrence
}

// CompileDonorTaskPlan compiles direct JSON and pinned physical image inputs. The caller must resolve
// their complete artifact closure, bind producer/consent/session/patch authority
// and seal a qualified task before any publication or execution.
func CompileDonorTaskPlan(ctx context.Context, base, donor v2input.SessionVersionBinding,
	sourceRaw, donorRaw, paramsRaw []byte, patchWidth, patchHeight int64, runtimeSHA string,
	capacity DonorResourceEstimate) (DonorTaskPlan, error) {
	var zero DonorTaskPlan
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if len(paramsRaw) < 2 || len(paramsRaw) > 65536 {
		return zero, v2input.ErrInvalid
	}
	var p OpenEyesParams
	if json.Unmarshal(paramsRaw, &p) != nil {
		return zero, v2input.ErrInvalid
	}
	body, digest, err := p.Compile()
	normalized, e := canonical.Bytes(paramsRaw)
	if err != nil || e != nil || !bytes.Equal(body, normalized) || p.Base != base || p.Donor != donor {
		return zero, v2input.ErrInvalid
	}
	inputs, err := DonorDirectInputs(p)
	if err != nil {
		return zero, err
	}
	physical, err := DonorImageInputs(p, sourceRaw, donorRaw)
	if err != nil {
		return zero, err
	}
	occurrences, err := DonorImageOccurrences(p, sourceRaw, donorRaw)
	if err != nil {
		return zero, err
	}
	profile, err := CompileDonorExecutionProfile(p, sourceRaw, donorRaw, patchWidth, patchHeight, runtimeSHA, capacity)
	if err != nil {
		return zero, err
	}
	var header struct {
		Resources DonorResourceEstimate `json:"resource_estimate"`
	}
	if json.Unmarshal(profile, &header) != nil {
		return zero, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return DonorTaskPlan{LocalTaskPlan: LocalTaskPlan{Operation: "portrait.open_eyes", Params: body,
		ParamsSHA: digest, Profile: profile, ReservedBytes: header.Resources.OutputBytes}, Inputs: inputs, PhysicalImageInputs: physical, PhysicalImageOccurrences: occurrences}, nil
}

// DonorDirectInputs validates distinct typed JSON roots before any issuer/catalog IO.
func DonorDirectInputs(p OpenEyesParams) ([]v2input.Ref, error) {
	if _, _, err := p.Compile(); err != nil {
		return nil, err
	}
	inputs := []v2input.Ref{p.Base.Source, p.Donor.Source, p.EyesMask, p.Protected,
		p.Confirmation, p.SourceFaces, p.DonorFaces}
	seen := make(map[uuid.UUID]bool, len(inputs))
	for _, ref := range inputs {
		if seen[ref.ArtifactID] {
			return nil, v2input.ErrInvalid
		}
		seen[ref.ArtifactID] = true
	}
	sort.Slice(inputs, func(i, j int) bool { return inputs[i].ArtifactID.String() < inputs[j].ArtifactID.String() })

	return inputs, nil
}
