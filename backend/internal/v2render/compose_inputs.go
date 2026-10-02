package v2render

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"sort"
)

// CompositionInputRoots enumerates documents directly read by the layer worker.
// A scoped closure resolver must expand WorkingImage/ImageResult/MaskRef children
// (pixels, alpha, reports, generated masks, transform fields and report evidence)
// before publishing the final task InputClosure. Roots alone are not sufficient.
func CompositionInputRoots(compiled CompiledComposition, paramsRef v2input.Ref) ([]v2input.Entry, error) {
	sum := sha256.Sum256(compiled.JSON)
	if !validLayerArtifactRef(paramsRef) || hex.EncodeToString(sum[:]) != paramsRef.SHA256 {
		return nil, v2input.ErrInvalid
	}
	params := compiled.Params
	body, err := canonical.Marshal(params)
	if err != nil || !bytes.Equal(body, compiled.JSON) {
		return nil, v2input.ErrInvalid
	}
	// Recompile the wire expression so an altered cached budget cannot omit
	// readable masks from the inventory.
	layer := Layer{ID: paramsRef.ArtifactID, Enabled: true, Opacity: params.Opacity, Blend: params.Blend, Operation: "layer.compose", OperationVersion: "1", Params: paramsRef, Mask: params.Mask}
	verified, err := CompileComposition(layer, params.Base, params.SourceResult, params.ProposalResult, params.ProtectedMask, 1, 1)
	if err != nil || params.SchemaVersion != 1 {
		return nil, v2input.ErrInvalid
	}
	entries := []v2input.Entry{{Ref: paramsRef, Contract: "Params"}, {Ref: params.Base.Source, Contract: "WorkingImage"}, {Ref: params.ProposalResult, Contract: "ImageResult"}}
	if params.SourceResult != nil {
		entries = append(entries, v2input.Entry{Ref: *params.SourceResult, Contract: "ImageResult"})
	}
	if params.ProtectedMask != nil {
		entries = append(entries, v2input.Entry{Ref: *params.ProtectedMask, Contract: "MaskRef"})
	}
	for _, node := range verified.Budget.Masks {
		if node.Operation == "ref" {
			if node.Ref == nil {
				return nil, v2input.ErrInvalid
			}
			entries = append(entries, v2input.Entry{Ref: *node.Ref, Contract: "MaskRef"})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		return bytes.Compare(entries[i].Ref.ArtifactID[:], entries[j].Ref.ArtifactID[:]) < 0
	})
	result := make([]v2input.Entry, 0, len(entries))
	for _, entry := range entries {
		if !validLayerArtifactRef(entry.Ref) {
			return nil, v2input.ErrInvalid
		}
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
