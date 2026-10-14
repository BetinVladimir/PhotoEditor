package v2render

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// RenderLayerDocument retains a direct occurrence, not permission to its descendants.
type RenderLayerDocument struct {
	Path     string      `json:"path"`
	Contract string      `json:"contract"`
	Ref      v2input.Ref `json:"ref"`
	Parent   v2input.Ref `json:"parent"`
	JCS      string      `json:"jcs"`
}

func renderLayerDocumentRefs(evidence CompiledRenderEvidence, raw []byte) ([]v2draft.LayerRef, error) {
	var authored struct {
		Stack *v2input.Ref `json:"layer_stack"`
	}
	if json.Unmarshal(evidence.Recipe.Recipe, &authored) != nil {
		return nil, v2input.ErrInvalid
	}
	if authored.Stack == nil {
		if len(raw) != 0 {
			return nil, v2input.ErrInvalid
		}
		return nil, nil
	}
	if len(raw) < 2 || len(raw) > 1048576 {
		return nil, v2input.ErrInvalid
	}
	sum := sha256.Sum256(raw)
	wire, err := canonical.Bytes(raw)
	if len(raw) < 2 || len(raw) > 1048576 || hex.EncodeToString(sum[:]) != authored.Stack.SHA256 || err != nil || !bytes.Equal(wire, raw) {
		return nil, v2input.ErrInvalid
	}
	if _, err = v2draft.ValidateLayerStack(raw, evidence.Recipe.Scalars.Source); err != nil {
		return nil, err
	}
	refs, err := v2draft.DocumentChildren(raw, "LayerStack", *authored.Stack)
	if err != nil || len(refs) < 1 || refs[0].Contract != "WorkingImage" || refs[0].Ref != evidence.Recipe.Scalars.Source.Source {
		return nil, v2input.ErrInvalid
	}
	return refs[1:], nil
}

// ValidateRenderLayerDocuments checks direct byte/binding evidence. Recursive
// closure scope, operation-specific Params schemas and binary masks remain required.
func ValidateRenderLayerDocuments(evidence CompiledRenderEvidence, raw []byte, documents []RenderLayerDocument) error {
	refs, err := renderLayerDocumentRefs(evidence, raw)
	if err != nil || len(refs) != len(documents) {
		return v2input.ErrInvalid
	}
	total := 0
	for i, document := range documents {
		ref := refs[i]
		if len(document.JCS) < 2 || len(document.JCS) > 1048576-total {
			return v2input.ErrInvalid
		}
		body := []byte(document.JCS)
		total += len(body)
		sum := sha256.Sum256(body)
		wire, e := canonical.Bytes(body)
		if document.Path != ref.Path || document.Contract != ref.Contract || document.Ref != ref.Ref || document.Parent != ref.ParentRef ||
			len(body) < 2 || total > 1048576 || hex.EncodeToString(sum[:]) != ref.Ref.SHA256 || e != nil || !bytes.Equal(wire, body) {
			return v2input.ErrInvalid
		}
		switch ref.Contract {
		case "Params":
			var fields map[string]json.RawMessage
			if json.Unmarshal(body, &fields) != nil || fields == nil {
				return v2input.ErrInvalid
			}
		case "MaskRef":
			mask, e := review.ParseMaskRef(body)
			if e != nil {
				return v2input.ErrInvalid
			}
			canvas, e := canonical.Hash(mask.Canvas)
			chain, e2 := canonical.Hash(json.RawMessage(mask.TransformChain))
			sourceChain, e3 := canonical.Hash(json.RawMessage(evidence.Source.TransformChain))
			if e != nil || e2 != nil || e3 != nil || canvas != evidence.Recipe.Scalars.Source.CanvasHash || chain != sourceChain {
				return v2input.ErrInvalid
			}
		default:
			return v2input.ErrInvalid
		}
		// A pinned document must also expose a well-formed descendant graph.
		// This is structural validation only; closure resolution still authorizes
		// each occurrence and verifies its independently pinned bytes.
		if _, e := v2draft.DocumentChildren(body, ref.Contract, ref.Ref); e != nil {
			return v2input.ErrInvalid
		}
	}
	return nil
}

func loadRenderLayerDocuments(evidence CompiledRenderEvidence, raw []byte, read func(v2draft.LayerRef) ([]byte, error)) ([]RenderLayerDocument, error) {
	refs, err := renderLayerDocumentRefs(evidence, raw)
	if err != nil {
		return nil, err
	}
	documents := make([]RenderLayerDocument, 0, len(refs))
	total := 0
	for _, ref := range refs {
		body, err := read(ref)
		if err != nil {
			return nil, err
		}
		total += len(body)
		if total > 1048576 {
			return nil, v2input.ErrInvalid
		}
		documents = append(documents, RenderLayerDocument{ref.Path, ref.Contract, ref.Ref, ref.ParentRef, string(body)})
	}
	if err := ValidateRenderLayerDocuments(evidence, raw, documents); err != nil {
		return nil, err
	}
	return documents, nil
}

// validateRenderLayerInventory ties frozen witnesses and their discovered direct
// descendants to the task inventory. It does not confer occurrence authority.
func validateRenderLayerInventory(documents []RenderLayerDocument, entries []v2input.Entry) error {
	available := make(map[v2input.Ref]string, len(entries))
	for _, entry := range entries {
		available[entry.Ref] = entry.Contract
	}
	check := func(ref v2input.Ref, contract string) bool {
		actual, ok := available[ref]
		if !ok {
			return false
		}
		switch contract {
		case "WorkingPixels", "WorkingAlpha", "MaskPixels", "DenseWarpField":
			contract = "NpyArray"
		case "Artifact":
			return true
		}
		return actual == contract
	}
	for _, document := range documents {
		if !check(document.Ref, document.Contract) {
			return v2input.ErrInvalid
		}
		children, err := v2draft.DocumentChildren([]byte(document.JCS), document.Contract, document.Ref)
		if err != nil {
			return err
		}
		for _, child := range children {
			if !check(child.Ref, child.Contract) {
				return v2input.ErrInvalid
			}
		}
	}
	return nil
}
