package v2portrait

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"sort"
)

// DonorImageOccurrence retains both version scopes when arrays are shared.
// Transport deduplication must not deduplicate authorization of manifest edges.
type DonorImageOccurrence struct {
	Binding v2input.SessionVersionBinding
	Parent  v2input.Ref
	Entry   v2input.Entry
	Role    string
}

func DonorImageOccurrences(params OpenEyesParams, sourceRaw, donorRaw []byte) ([]DonorImageOccurrence, error) {
	if _, _, err := params.Compile(); err != nil {
		return nil, err
	}
	source, err := VerifyDonorWorkingImage(params.Base, sourceRaw)
	if err != nil {
		return nil, err
	}
	donor, err := VerifyDonorWorkingImage(params.Donor, donorRaw)
	if err != nil || source.Color != donor.Color {
		return nil, v2input.ErrInvalid
	}
	result := make([]DonorImageOccurrence, 0, 4)
	appendRef := func(binding v2input.SessionVersionBinding, id uuid.UUID, sha, role string) {
		result = append(result, DonorImageOccurrence{Binding: binding, Parent: binding.Source, Role: role,
			Entry: v2input.Entry{Ref: v2input.Ref{ArtifactID: id, SHA256: sha}, Contract: "NpyArray"}})
	}
	appendRef(params.Base, source.Pixels.ID, source.Pixels.SHA256, "WorkingPixels")
	if source.Alpha != nil {
		appendRef(params.Base, source.Alpha.ID, source.Alpha.SHA256, "WorkingAlpha")
	}
	appendRef(params.Donor, donor.Pixels.ID, donor.Pixels.SHA256, "WorkingPixels")
	if donor.Alpha != nil {
		appendRef(params.Donor, donor.Alpha.ID, donor.Alpha.SHA256, "WorkingAlpha")
	}
	return result, nil
}

// DonorImageInputs extracts physical RGB/alpha refs from both pinned manifests.
// It is one part of closure resolution, not a complete task inventory: geometry,
// masks, original producer, confirmation and observations must also be traversed.
func DonorImageInputs(params OpenEyesParams, sourceRaw, donorRaw []byte) ([]v2input.Entry, error) {
	occurrences, err := DonorImageOccurrences(params, sourceRaw, donorRaw)
	if err != nil {
		return nil, err
	}
	refs := make([]v2input.Ref, 0, len(occurrences))
	for _, occurrence := range occurrences {
		refs = append(refs, occurrence.Entry.Ref)
	}
	seen := map[uuid.UUID]string{}
	entries := make([]v2input.Entry, 0, len(refs))
	direct := map[uuid.UUID]bool{}
	for _, ref := range []v2input.Ref{params.Base.Source, params.Donor.Source, params.EyesMask,
		params.Protected, params.Confirmation, params.SourceFaces, params.DonorFaces} {
		direct[ref.ArtifactID] = true
	}
	for _, ref := range refs {
		if direct[ref.ArtifactID] {
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
