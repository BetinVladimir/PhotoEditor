package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

// applyItemMaskInput is immutable run configuration produced after the API has
// authorized each retained override against its exact selected source. It is
// not an accepted receipt and does not create permission to publish one.
type applyItemMaskInput struct {
	ItemID         uuid.UUID       `json:"item_id"`
	AssetVersionID uuid.UUID       `json:"asset_version_id"`
	InputSHA       string          `json:"input_sha256"`
	BodySHA        string          `json:"body_sha256"`
	Body           json.RawMessage `json:"body"`
}

func boundApplyItemMasks(items []applyInput, inputs []applyItemMaskInput) (map[uuid.UUID][]applyOverrideMaskBinding, error) {
	out := map[uuid.UUID][]applyOverrideMaskBinding{}
	if len(inputs) == 0 {
		return out, nil
	}
	if len(inputs) != len(items) {
		return nil, errors.New("item mask source count mismatch")
	}
	byID := map[uuid.UUID]applyInput{}
	for _, item := range items {
		if item.ItemID == uuid.Nil || byID[item.ItemID].ItemID != uuid.Nil {
			return nil, errors.New("duplicate source item")
		}
		byID[item.ItemID] = item
	}
	for _, input := range inputs {
		item, exists := byID[input.ItemID]
		if !exists || out[input.ItemID] != nil || input.AssetVersionID != item.VersionID || input.InputSHA != item.Image.SHA256 {
			return nil, errors.New("item mask source binding mismatch")
		}
		body, err := canonical.Bytes(input.Body)
		if err != nil || !bytes.Equal(body, input.Body) {
			return nil, errors.New("noncanonical item mask body")
		}
		pin, err := canonical.Hash(json.RawMessage(body))
		if err != nil || pin != input.BodySHA {
			return nil, errors.New("item mask body hash mismatch")
		}
		var envelope struct {
			InputSHA string                     `json:"input_sha256"`
			Masks    []applyOverrideMaskBinding `json:"mask_refs"`
		}
		if json.Unmarshal(body, &envelope) != nil || envelope.InputSHA != input.InputSHA || len(envelope.Masks) == 0 || len(envelope.Masks) > 128 {
			return nil, errors.New("item mask body invalid")
		}
		seen := map[string]bool{}
		for _, binding := range envelope.Masks {
			key := binding.BindingID
			if binding.BindingID == "" || seen[key] || binding.Mask.Artifact.ID == uuid.Nil || binding.Mask.Canvas != item.Working.Canvas || !bytes.Equal(binding.Mask.TransformChain, item.Working.TransformChain) {
				return nil, errors.New("item mask geometry or identity mismatch")
			}
			seen[key] = true
			if binding.Purpose == "protected" {
				if !binding.Mask.Protected {
					return nil, errors.New("unprotected guard mask")
				}
			} else if binding.Mask.Protected || !binding.Confirmed {
				return nil, errors.New("unapproved item target mask")
			}
		}
		out[input.ItemID] = envelope.Masks
	}
	return out, nil
}
