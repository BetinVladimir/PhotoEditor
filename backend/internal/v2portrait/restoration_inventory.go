package v2portrait

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/google/uuid"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// BuildRestorationInputInventory derives the exact direct and physical roots
// from verified source/mask documents. No client-provided inventory is trusted.
// The caller still needs scoped version lookups for every physical array and
// the compiler/SQL graph gate before admitting the task.
func BuildRestorationInputInventory(ctx context.Context, operation string,
	paramsRaw []byte, paramsRef v2input.Ref, pin RestorationQualificationPin,
	documents RestorationDocuments) ([]v2input.Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := pin.Compile(); err != nil || pin.Operation != operation {
		return nil, v2input.ErrInvalid
	}
	var p struct {
		Base        v2input.SessionVersionBinding  `json:"base"`
		Donor       *v2input.SessionVersionBinding `json:"donor"`
		Mask        v2input.Ref                    `json:"mask"`
		GlassesMask v2input.Ref                    `json:"glasses_mask"`
		Protected   v2input.Ref                    `json:"protected"`
	}
	if json.Unmarshal(paramsRaw, &p) != nil {
		return nil, v2input.ErrInvalid
	}
	direct, _, err := restorationDirectInputs(operation, paramsRaw)
	if err != nil {
		return nil, err
	}
	source, err := VerifyDonorWorkingImage(p.Base, documents.Source)
	if err != nil {
		return nil, v2input.ErrInvalid
	}
	images := []review.WorkingImage{source}
	if operation == "portrait.glare" && p.Donor != nil {
		donor, err := VerifyDonorWorkingImage(*p.Donor, documents.Donor)
		if err != nil || donor.Color != source.Color {
			return nil, v2input.ErrInvalid
		}
		images = append(images, donor)
	} else if p.Donor != nil || len(documents.Donor) != 0 {
		return nil, v2input.ErrInvalid
	}
	targetRef := p.Mask
	if operation == "portrait.glare" {
		targetRef = p.GlassesMask
	}
	target, err := restorationMask(documents.Target, targetRef, source, false)
	if err != nil {
		return nil, err
	}
	guard, err := restorationMask(documents.Protected, p.Protected, source, true)
	if err != nil || target.Artifact.ID == guard.Artifact.ID {
		return nil, v2input.ErrInvalid
	}
	entries := append([]v2input.Entry(nil), direct...)
	entries = append(entries,
		v2input.Entry{Ref: paramsRef, Contract: "Params"},
		v2input.Entry{Ref: pin.Report, Contract: "Report"},
		v2input.Entry{Ref: v2input.Ref{ArtifactID: target.Artifact.ID,
			SHA256: target.Artifact.SHA256}, Contract: "NpyArray"},
		v2input.Entry{Ref: v2input.Ref{ArtifactID: guard.Artifact.ID,
			SHA256: guard.Artifact.SHA256}, Contract: "NpyArray"})
	for _, image := range images {
		arrays, err := restorationImageArrays(image)
		if err != nil {
			return nil, err
		}
		for _, ref := range arrays {
			entries = append(entries, v2input.Entry{Ref: ref, Contract: "NpyArray"})
		}
	}
	owned := make(map[uuid.UUID]v2input.Entry, len(entries))
	for _, entry := range entries {
		if entry.Ref.ArtifactID == uuid.Nil || entry.Contract == "" {
			return nil, v2input.ErrInvalid
		}
		if _, err := domain.ParseDigest(entry.Ref.SHA256); err != nil {
			return nil, v2input.ErrInvalid
		}
		if previous, found := owned[entry.Ref.ArtifactID]; found {
			if previous != entry {
				return nil, v2input.ErrInvalid
			}
		} else {
			owned[entry.Ref.ArtifactID] = entry
		}
	}
	if len(owned) < 6 || len(owned) > 127 {
		return nil, v2input.ErrInvalid
	}
	result := make([]v2input.Entry, 0, len(owned))
	for _, entry := range owned {
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Ref.ArtifactID.String() < result[j].Ref.ArtifactID.String()
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
