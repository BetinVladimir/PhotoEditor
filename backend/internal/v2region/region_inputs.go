package v2region

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"sort"
)

// Called only after CompileRegionEvidence has validated the complete image and
// transform chain. No recursive scan of arbitrary JSON can grant worker inputs.
func regionInputRequirements(source, recipe v2input.Ref, evidence CompiledRegionEvidence) ([]v2input.Entry, error) {
	entries := []v2input.Entry{{Ref: source, Contract: "WorkingImage"}, {Ref: recipe, Contract: "FrozenRecipe"}, {Ref: evidence.Recipe.BaseStyle, Contract: "StyleV2"},
		{Ref: v2input.Ref{ArtifactID: evidence.Source.Pixels.ID, SHA256: evidence.Source.Pixels.SHA256}, Contract: "NpyArray"}}
	if alpha := evidence.Source.Alpha; alpha != nil {
		entries = append(entries, v2input.Entry{Ref: v2input.Ref{ArtifactID: alpha.ID, SHA256: alpha.SHA256}, Contract: "NpyArray"})
	}
	var chain struct {
		Steps []struct {
			InverseField *v2input.Ref `json:"inverse_field"`
		} `json:"steps"`
	}
	if json.Unmarshal(evidence.Source.TransformChain, &chain) != nil {
		return nil, ErrRegionInvalid
	}
	for _, step := range chain.Steps {
		if step.InverseField != nil {
			entries = append(entries, v2input.Entry{Ref: *step.InverseField, Contract: "NpyArray"})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		return bytes.Compare(entries[i].Ref.ArtifactID[:], entries[j].Ref.ArtifactID[:]) < 0
	})
	unique := make([]v2input.Entry, 0, len(entries))
	for _, entry := range entries {
		digest, err := hex.DecodeString(entry.Ref.SHA256)
		if entry.Ref.ArtifactID == uuid.Nil || err != nil || len(digest) != 32 || hex.EncodeToString(digest) != entry.Ref.SHA256 {
			return nil, ErrRegionInvalid
		}
		if len(unique) > 0 && unique[len(unique)-1].Ref.ArtifactID == entry.Ref.ArtifactID {
			if unique[len(unique)-1] != entry {
				return nil, ErrRegionInvalid
			}
			continue
		}
		unique = append(unique, entry)
	}
	return unique, nil
}
