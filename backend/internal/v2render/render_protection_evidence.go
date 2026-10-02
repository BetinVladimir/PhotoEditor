package v2render

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// ValidateRenderProtectionEvidence pins typed metadata, canvas and transform.
// Scoped provenance and binary mask values remain separate admission checks.
func ValidateRenderProtectionEvidence(evidence CompiledRenderEvidence, raw []byte) error {
	var authored struct {
		Protection *v2input.Ref `json:"protected_mask"`
	}
	if json.Unmarshal(evidence.Recipe.Recipe, &authored) != nil {
		return v2input.ErrInvalid
	}
	if authored.Protection == nil {
		if len(raw) != 0 {
			return v2input.ErrInvalid
		}
		return nil
	}
	if len(raw) < 2 || len(raw) > 1048576 {
		return v2input.ErrInvalid
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != authored.Protection.SHA256 {
		return v2input.ErrInvalid
	}
	wire, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(wire, raw) {
		return v2input.ErrInvalid
	}
	mask, err := review.ParseMaskRef(raw)
	if err != nil || !mask.Protected {
		return v2input.ErrInvalid
	}
	canvas, err := canonical.Hash(mask.Canvas)
	if err != nil || canvas != evidence.Recipe.Scalars.Source.CanvasHash {
		return v2input.ErrInvalid
	}
	chain, err := canonical.Hash(json.RawMessage(mask.TransformChain))
	if err != nil {
		return v2input.ErrInvalid
	}
	sourceChain, err := canonical.Hash(json.RawMessage(evidence.Source.TransformChain))
	if err != nil || chain != sourceChain {
		return v2input.ErrInvalid
	}
	return nil
}
