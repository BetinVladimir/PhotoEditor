package v2draft

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// PreparedDraft holds the exact canonical bytes to publish. LayerStack is
// published first; its returned reference is then inserted into Recipe.
type PreparedDraft struct {
	Recipe []byte
	Layers []byte
	Base   []byte
}

// PrepareDraft rejects a client-supplied layer reference when the layers body
// is null. When layers are present, layerRef must be the published body itself.
func PrepareDraft(recipe, layers []byte, binding v2input.SessionVersionBinding,
	layerRef *v2input.Ref, first bool) (PreparedDraft, error) {
	var out PreparedDraft
	if len(recipe) == 0 || len(layers) == 0 {
		return out, v2input.ErrInvalid
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(recipe, &fields); err != nil || fields == nil {
		return out, v2input.ErrInvalid
	}
	if bytes.Equal(bytes.TrimSpace(layers), []byte("null")) {
		if layerRef != nil || string(fields["layer_stack"]) != "null" {
			return out, v2input.ErrInvalid
		}
	} else {
		if layerRef == nil || !isDocumentRef(*layerRef) {
			return out, v2input.ErrInvalid
		}
		canonicalLayers, err := canonical.Bytes(layers)
		if err != nil || len(canonicalLayers) > 1048576 {
			return out, v2input.ErrInvalid
		}
		digest := sha256.Sum256(canonicalLayers)
		if layerRef.SHA256 != hex.EncodeToString(digest[:]) {
			return out, v2input.ErrInvalid
		}
		if _, err = ValidateLayerStack(canonicalLayers, binding); err != nil {
			return out, err
		}
		out.Layers = canonicalLayers
		fields["layer_stack"], err = json.Marshal(layerRef)
		if err != nil {
			return PreparedDraft{}, err
		}
	}
	var err error
	out.Recipe, err = canonical.Marshal(fields)
	if err != nil || len(out.Recipe) > 1048576 ||
		v2input.ValidateEditingRecipeV3(out.Recipe, binding) != nil {
		return PreparedDraft{}, v2input.ErrInvalid
	}
	if first {
		out.Base, err = BaseCheckpoint(out.Recipe, binding)
		if err != nil {
			return PreparedDraft{}, err
		}
	}
	return out, nil
}
