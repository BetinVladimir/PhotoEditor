package v2render

import (
	"bytes"
	"photoeditor/backend/internal/v2input"
	"sort"
)

// AuthoredSegmentInputRoots inventories actual published direct inputs only.
// Typed traversal and current canonical SQL authority are mandatory afterwards.
func AuthoredSegmentInputRoots(compiled CompiledAuthoredSegmentParams, paramsRef v2input.Ref) ([]v2input.Entry, error) {
	if ValidateAuthoredSegmentParams(compiled, paramsRef) != nil {
		return nil, v2input.ErrInvalid
	}
	p := compiled.Params
	entries := []v2input.Entry{{Ref: paramsRef, Contract: "Params"}, {Ref: p.Recipe, Contract: "FrozenRenderRecipe"}, {Ref: p.BaseReceipt, Contract: "AuthoredBaseReceipt"}, {Ref: p.Source.Source, Contract: "WorkingImage"}}
	if p.CurrentLayers != nil {
		entries = append(entries, v2input.Entry{Ref: *p.CurrentLayers, Contract: "LayerStack"})
	}
	if p.ProtectedMask != nil {
		entries = append(entries, v2input.Entry{Ref: *p.ProtectedMask, Contract: "MaskRef"})
	}
	for _, ref := range p.PhaseReceipts {
		entries = append(entries, v2input.Entry{Ref: ref, Contract: "AuthoredCanonicalPhaseReceipt"})
	}
	sort.Slice(entries, func(i, j int) bool {
		return bytes.Compare(entries[i].Ref.ArtifactID[:], entries[j].Ref.ArtifactID[:]) < 0
	})
	result := make([]v2input.Entry, 0, len(entries))
	for _, entry := range entries {
		if len(result) > 0 && result[len(result)-1].Ref.ArtifactID == entry.Ref.ArtifactID {
			if result[len(result)-1] != entry {
				return nil, v2input.ErrInvalid
			}
			continue
		}
		result = append(result, entry)
	}
	return result, nil
}
