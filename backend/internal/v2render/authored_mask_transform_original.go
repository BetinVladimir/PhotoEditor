package v2render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"strings"
)

func authoredMaskChainPrefix(original, current []json.RawMessage) bool {
	if len(current) < len(original) {
		return false
	}
	for i := range original {
		a, err := canonical.Bytes(original[i])
		if err != nil {
			return false
		}
		b, err := canonical.Bytes(current[i])
		if err != nil || !bytes.Equal(a, b) {
			return false
		}
	}
	return true
}

// Distinct witness modes never reinterpret a standalone MaskRef as PortraitParams.
// This is content/occurrence validation; the private SQL source gate independently
// repeats actual current program, document version and original occurrence proof.
func validateAuthoredMaskOriginalWitness(source AuthoredInstructionSource, d AuthoredMaskTransformDescriptor) error {
	global := d.OriginalProtectedRef != nil || d.OriginalProtectedJCS != nil
	layerMask := d.OriginalLayerMaskRef != nil || d.OriginalLayerMaskJCS != nil
	if global && layerMask || (global || layerMask) && d.OriginalRepairJCS != nil {
		return v2input.ErrInvalid
	}
	if !global && !layerMask {
		return nil
	} // existing inline portrait/defect SQL modes
	var ref *v2input.Ref
	var raw *string
	if global {
		ref = d.OriginalProtectedRef
		raw = d.OriginalProtectedJCS
	} else {
		ref = d.OriginalLayerMaskRef
		raw = d.OriginalLayerMaskJCS
	}
	if ref == nil || raw == nil || !validLayerArtifactRef(*ref) || len(*raw) < 2 || len(*raw) > 1048576 {
		return v2input.ErrInvalid
	}
	body, err := canonical.Bytes([]byte(*raw))
	if err != nil || !bytes.Equal(body, []byte(*raw)) {
		return v2input.ErrInvalid
	}
	digest, err := canonical.Hash(json.RawMessage(body))
	if err != nil || digest != ref.SHA256 {
		return v2input.ErrInvalid
	}
	parsed, err := review.ParseMaskRef(body)
	if err != nil {
		return err
	}
	actual, err := canonical.Marshal(parsed)
	if err != nil {
		return err
	}
	original, err := canonical.Marshal(d.OriginalMask)
	if err != nil || !bytes.Equal(actual, original) {
		return v2input.ErrInvalid
	}
	if global {
		if d.OriginalParams != source.OriginalRecipe || d.OriginalParamsJCS != source.RecipeJCS {
			return v2input.ErrInvalid
		}
		frozen, err := v2draft.DecodeFrozenRenderRecipe([]byte(source.RecipeJCS))
		if err != nil {
			return err
		}
		var recipe struct {
			Protected *v2input.Ref `json:"protected_mask"`
		}
		if json.Unmarshal(frozen.Recipe, &recipe) != nil || recipe.Protected == nil || *recipe.Protected != *ref {
			return v2input.ErrInvalid
		}
		return nil
	}
	var stack struct {
		Source v2input.SessionVersionBinding `json:"source"`
		Layers []struct {
			ID     uuid.UUID   `json:"id"`
			Params v2input.Ref `json:"params"`
		} `json:"layers"`
	}
	if json.Unmarshal([]byte(source.LayerStackJCS), &stack) != nil || len(source.Instruction.Layers) != 1 {
		return v2input.ErrInvalid
	}
	refs, err := v2draft.ValidateLayerStack([]byte(source.LayerStackJCS), stack.Source)
	if err != nil {
		return err
	}
	selected := -1
	for i, layer := range stack.Layers {
		if layer.ID == source.Instruction.Layers[0] {
			if selected != -1 || layer.Params != d.OriginalParams {
				return v2input.ErrInvalid
			}
			selected = i
		}
	}
	if selected < 0 {
		return v2input.ErrInvalid
	}
	prefix := fmt.Sprintf("LayerStack.layers[%d].mask", selected)
	for _, entry := range refs {
		if entry.Contract == "MaskRef" && entry.Ref == *ref && (entry.Path == prefix || strings.HasPrefix(entry.Path, prefix+".")) {
			return nil
		}
	}
	return v2input.ErrInvalid
}
