package v2optical

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"sort"
)

type PerspectiveImageOccurrence struct {
	Binding v2input.SessionVersionBinding
	Parent  v2input.Ref
	Entry   v2input.Entry
	Role    string
}

// PerspectiveImageInputs retains each manifest edge while deduplicating transport.
// Exact original object bytes and per-edge live SQL authority remain mandatory.
func PerspectiveImageInputs(params PerspectiveParams, sourceRaw []byte, report, calibration v2input.Ref) ([]v2input.Entry, []PerspectiveImageOccurrence, error) {
	image, err := VerifyPerspectiveWorkingImage(params, sourceRaw)
	if err != nil {
		return nil, nil, err
	}
	var chain struct {
		Steps []struct {
			InverseField *review.ArtifactRef `json:"inverse_field"`
		} `json:"steps"`
	}
	if json.Unmarshal(image.TransformChain, &chain) != nil {
		return nil, nil, v2input.ErrInvalid
	}
	var occurrences []PerspectiveImageOccurrence
	appendRef := func(ref review.ArtifactRef, role string) {
		occurrences = append(occurrences, PerspectiveImageOccurrence{
			Binding: params.Base, Parent: params.Base.Source, Role: role, Entry: v2input.Entry{Ref: v2input.Ref{ArtifactID: ref.ID, SHA256: ref.SHA256}, Contract: "NpyArray"}})
	}
	appendRef(image.Pixels, "WorkingPixels")
	if image.Alpha != nil {
		appendRef(*image.Alpha, "WorkingAlpha")
	}
	for i, step := range chain.Steps {
		if step.InverseField != nil {
			appendRef(*step.InverseField, fmt.Sprintf("InverseField/%d", i))
		}
	}
	roots := map[uuid.UUID]bool{params.Base.Source.ArtifactID: true, calibration.ArtifactID: true, report.ArtifactID: true}
	seen := map[uuid.UUID]string{}
	var entries []v2input.Entry
	for _, edge := range occurrences {
		ref := edge.Entry.Ref
		if roots[ref.ArtifactID] || ref.ArtifactID == uuid.Nil {
			return nil, nil, v2input.ErrInvalid
		}
		if old, ok := seen[ref.ArtifactID]; ok {
			if old != ref.SHA256 {
				return nil, nil, v2input.ErrInvalid
			}
			continue
		}
		seen[ref.ArtifactID] = ref.SHA256
		entries = append(entries, edge.Entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Ref.ArtifactID.String() < entries[j].Ref.ArtifactID.String() })
	return entries, occurrences, nil
}
