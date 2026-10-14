package semanticproposal

import (
	"encoding/json"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type defectDescriptorSnapshot struct {
	Published PublishedDefectParams
	Nodes     []v2draft.LayerRef
}

// Own every mutable dependency and nested authority field before catalog IO.
// This checks descriptor identity only; content and scoped provenance gates
// remain mandatory and cannot be replaced by a serialized snapshot.
func snapshotDefectDescriptors(published PublishedDefectParams, nodes []v2draft.LayerRef) (defectDescriptorSnapshot, error) {
	var zero defectDescriptorSnapshot
	if _, err := validateDefectProfileDescriptors(published, nodes); err != nil {
		return zero, err
	}
	body, err := canonical.Marshal(defectDescriptorSnapshot{Published: published, Nodes: nodes})
	if err != nil || len(body) > 4<<20 {
		return zero, v2input.ErrInvalid
	}
	var owned defectDescriptorSnapshot
	if json.Unmarshal(body, &owned) != nil {
		return zero, v2input.ErrInvalid
	}
	if _, err := validateDefectProfileDescriptors(owned.Published, owned.Nodes); err != nil {
		return zero, err
	}
	return owned, nil
}
