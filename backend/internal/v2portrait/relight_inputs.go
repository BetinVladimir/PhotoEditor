package v2portrait

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"sort"
)

// RelightSourceInputs extracts physical source dependencies from exact verified
// bytes. Live asset authority and depth/mask producer closure are separate.
func RelightSourceInputs(params RelightParams, pin RelightQualificationPin, sourceRaw []byte) ([]v2input.Entry, error) {
	roots, err := RelightRootInputs(params, pin)
	if err != nil {
		return nil, err
	}
	source, err := VerifyDonorWorkingImage(params.Base, sourceRaw)
	if err != nil {
		return nil, err
	}
	refs := []v2input.Ref{{ArtifactID: source.Pixels.ID, SHA256: source.Pixels.SHA256}}
	if source.Alpha != nil {
		refs = append(refs, v2input.Ref{ArtifactID: source.Alpha.ID, SHA256: source.Alpha.SHA256})
	}
	var chain struct {
		Steps []struct {
			Kind         string       `json:"kind"`
			InverseField *v2input.Ref `json:"inverse_field"`
		} `json:"steps"`
	}
	if json.Unmarshal(source.TransformChain, &chain) != nil {
		return nil, v2input.ErrInvalid
	}
	for _, step := range chain.Steps {
		if step.Kind == "dense_warp" {
			if step.InverseField == nil {
				return nil, v2input.ErrInvalid
			}
			refs = append(refs, *step.InverseField)
		}
	}
	rootIDs := map[uuid.UUID]bool{}
	for _, root := range roots {
		rootIDs[root.Ref.ArtifactID] = true
	}
	seen := map[uuid.UUID]string{}
	entries := make([]v2input.Entry, 0, len(refs))
	for _, ref := range refs {
		if rootIDs[ref.ArtifactID] {
			return nil, v2input.ErrInvalid
		}
		if old, ok := seen[ref.ArtifactID]; ok {
			if old != ref.SHA256 {
				return nil, v2input.ErrInvalid
			}
			continue
		}
		seen[ref.ArtifactID] = ref.SHA256
		entries = append(entries, v2input.Entry{Ref: ref, Contract: "NpyArray"})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Ref.ArtifactID.String() < entries[j].Ref.ArtifactID.String() })
	return entries, nil
}

// RelightRootInputs freezes typed document roots. This is not the complete
// closure: masks, depth, source geometry, original producers and binary children
// must be resolved from verified documents before transport or admission.
func RelightRootInputs(params RelightParams, pin RelightQualificationPin) ([]v2input.Entry, error) {
	if _, _, err := params.Compile(); err != nil {
		return nil, err
	}
	if _, err := pin.Compile(); err != nil {
		return nil, err
	}
	if params.Calibration != pin.Calibration {
		return nil, v2input.ErrInvalid
	}
	entries := []v2input.Entry{
		{Ref: params.Base.Source, Contract: "WorkingImage"},
		{Ref: params.Depth, Contract: "RelativeDepth"},
		{Ref: params.Subject, Contract: "MaskRef"},
		{Ref: params.Protected, Contract: "MaskRef"},
		{Ref: params.Calibration, Contract: "Calibration"},
		{Ref: pin.Report, Contract: "QualificationReport"},
	}
	seen := map[uuid.UUID]bool{}
	for _, entry := range entries {
		if seen[entry.Ref.ArtifactID] {
			return nil, v2input.ErrInvalid
		}
		seen[entry.Ref.ArtifactID] = true
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Ref.ArtifactID.String() < entries[j].Ref.ArtifactID.String() })
	return entries, nil
}
