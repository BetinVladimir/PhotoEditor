package v2render

import (
	"encoding/json"
	"photoeditor/backend/internal/v2input"
)

// verifyAuthoredGeometryPrefixDocuments reuses only the pure root3/mapping2
// byte validator. The DTO conversion below never reads a final inventory,
// final task or final adoption scope: geometry SQL selected every input above.
func verifyAuthoredGeometryPrefixDocuments(d AuthoredGeometryDocuments) error {
	var base struct {
		Original v2input.SessionVersionBinding `json:"original_source"`
	}
	if json.Unmarshal([]byte(d.Inventory.BaseReceiptJCS), &base) != nil || !authoredBindingValid(base.Original) {
		return v2input.ErrInvalid
	}
	content := AuthoredFinalDocuments{Inventory: AuthoredFinalInventory{Context: d.Inventory.Context, BaseReceipt: d.Inventory.BaseReceipt, BaseReceiptJCS: d.Inventory.BaseReceiptJCS, Phases: d.Inventory.Phases}, Bodies: d.Bodies}
	prior := []v2input.Ref{}
	for _, item := range d.Inventory.Phases {
		var phase AuthoredPhaseReceipt
		if decodeAuthoredParams([]byte(item.ReceiptJCS), &phase) != nil {
			return v2input.ErrInvalid
		}
		if phase.Kind == "geometry" {
			if e := verifyAuthoredFinalRoot3Mapping(content, phase, d.Bodies[phase.Result], base.Original, prior); e != nil {
				return e
			}
		}
		prior = append(prior, item.Receipt)
	}
	return nil
}
