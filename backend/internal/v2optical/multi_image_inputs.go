package v2optical

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"sort"
)

type MultiImageOccurrence struct {
	Ordinal int
	Binding v2input.SessionVersionBinding
	Parent  v2input.Ref
	Entry   v2input.Entry
	Role    string
}

// MultiImageInputs retains every frame/manifest edge while deduplicating transport.
// Object headers/bytes, dimensions and live SQL authority remain per-edge gates.
func MultiImageInputs(operation string, paramsRaw []byte, originals [][]byte, report v2input.Ref) ([]v2input.Entry, []MultiImageOccurrence, error) {
	images, err := VerifyMultiWorkingImages(operation, paramsRaw, originals)
	if err != nil {
		return nil, nil, err
	}
	var frames []v2input.SessionVersionBinding
	var calibration v2input.Ref
	if operation == "optical.panorama" {
		p, _ := ParsePanoramaParams(paramsRaw)
		frames = p.Frames
		calibration = p.Calibration
	} else {
		p, _ := ParseFocusStackParams(paramsRaw)
		frames = p.Frames
		calibration = p.Calibration
	}
	if !validPerspectiveRef(report) || report.ArtifactID == calibration.ArtifactID {
		return nil, nil, v2input.ErrInvalid
	}
	roots := map[uuid.UUID]string{report.ArtifactID: report.SHA256, calibration.ArtifactID: calibration.SHA256}
	for _, frame := range frames {
		if frame.Source.ArtifactID == report.ArtifactID || frame.Source.ArtifactID == calibration.ArtifactID {
			return nil, nil, v2input.ErrInvalid
		}
		if old, ok := roots[frame.Source.ArtifactID]; ok && old != frame.Source.SHA256 {
			return nil, nil, v2input.ErrInvalid
		}
		roots[frame.Source.ArtifactID] = frame.Source.SHA256
	}
	var occurrences []MultiImageOccurrence
	for ordinal, image := range images {
		appendRef := func(ref review.ArtifactRef, role string) {
			occurrences = append(occurrences, MultiImageOccurrence{Ordinal: ordinal, Binding: frames[ordinal], Parent: frames[ordinal].Source,
				Entry: v2input.Entry{Ref: v2input.Ref{ArtifactID: ref.ID, SHA256: ref.SHA256}, Contract: "NpyArray"}, Role: role})
		}
		if image.Alpha != nil && image.Alpha.ID == image.Pixels.ID {
			return nil, nil, v2input.ErrInvalid
		}
		appendRef(image.Pixels, "WorkingPixels")
		if image.Alpha != nil {
			appendRef(*image.Alpha, "WorkingAlpha")
		}
		var chain struct {
			Steps []struct {
				InverseField *review.ArtifactRef `json:"inverse_field"`
			} `json:"steps"`
		}
		if json.Unmarshal(image.TransformChain, &chain) != nil {
			return nil, nil, v2input.ErrInvalid
		}
		for index, step := range chain.Steps {
			if step.InverseField != nil {
				appendRef(*step.InverseField, fmt.Sprintf("InverseField/%d", index))
			}
		}
	}
	seen := map[uuid.UUID]string{}
	var entries []v2input.Entry
	for _, edge := range occurrences {
		ref := edge.Entry.Ref
		if _, ok := roots[ref.ArtifactID]; ok || !validPerspectiveRef(ref) {
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
