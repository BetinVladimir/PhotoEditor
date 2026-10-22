package v2draft

import (
	"bytes"
	"encoding/json"
	"photoeditor/backend/internal/v2input"
)

// This supplies typed edges only. SQL/catalog scope must independently prove
// the canonical predecessor and every actual published current-layer document.
func authoredSegmentRefs(raw []byte, path string, parent v2input.Ref) ([]LayerRef, error) {
	f, err := authoredObject(raw, "schema_version tenant_id parent_run_id workflow_id instruction_id ordinal program_sha256 predecessor_sha256 recipe base_receipt source current_layers protected_mask phase_receipts")
	if err != nil {
		return nil, err
	}
	if _, ok := authoredInt(f["schema_version"], 1, 1); !ok {
		return nil, v2input.ErrInvalid
	}
	for _, key := range []string{"tenant_id", "parent_run_id", "workflow_id", "instruction_id"} {
		if _, ok := authoredUUID(f[key]); !ok {
			return nil, v2input.ErrInvalid
		}
	}
	ordinal, ok := authoredInt(f["ordinal"], 1, 4998)
	if !ok || !authoredSHA(f["program_sha256"]) || !authoredSHA(f["predecessor_sha256"]) {
		return nil, v2input.ErrInvalid
	}
	a := authoredEdges{path: path, parent: parent}
	a.add(".recipe", "FrozenRenderRecipe", f["recipe"])
	a.add(".base_receipt", "AuthoredBaseReceipt", f["base_receipt"])
	a.binding(".source", f["source"])
	if !bytes.Equal(f["current_layers"], []byte("null")) {
		a.add(".current_layers", "LayerStack", f["current_layers"])
	}
	if !bytes.Equal(f["protected_mask"], []byte("null")) {
		a.add(".protected_mask", "MaskRef", f["protected_mask"])
	}
	refs := a.refs(".phase_receipts", "AuthoredCanonicalPhaseReceipt", f["phase_receipts"], 0, 4997, true)
	if int64(len(refs)) != ordinal-1 {
		return nil, v2input.ErrInvalid
	}
	if a.err != nil {
		return nil, a.err
	}
	return a.children, nil
}

// Distinct segment markers prevent malformed segment Params falling back to
// generic opaque discovery. Final-compose Params have neither marker.
func authoredSegmentParamRefs(raw []byte, path string, parent v2input.Ref) ([]LayerRef, bool, error) {
	var f map[string]json.RawMessage
	if json.Unmarshal(raw, &f) != nil {
		return nil, false, v2input.ErrInvalid
	}
	_, predecessor := f["predecessor_sha256"]
	_, layers := f["current_layers"]
	if !predecessor && !layers {
		return nil, false, nil
	}
	refs, err := authoredSegmentRefs(raw, path, parent)
	return refs, true, err
}
