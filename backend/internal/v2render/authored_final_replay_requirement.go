package v2render

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Derive replay requirements from the complete actual ordered prefix, never
// from whichever ready replay rows happen to exist. Input inventory is supplied
// by ReadAuthoredFinalInventory and is rechecked again by protected preparation.
func authoredFinalRequiresNativeReplay(inventory AuthoredFinalInventory, base AuthoredFinalTaskPlan) (bool, error) {
	if base.validateFrozen() != nil || inventory.Context.Tenant != base.Params.Params.TenantID || inventory.Context.Parent != base.Params.Params.ParentRunID || inventory.Context.Instruction.Identity != base.Params.Params.InstructionID || len(inventory.Phases) != len(base.Params.Params.PhaseReceipts) {
		return false, v2input.ErrInvalid
	}
	required := false
	for i, item := range inventory.Phases {
		if item.Ordinal != i+1 || item.Receipt != base.Params.Params.PhaseReceipts[i] {
			return false, v2input.ErrInvalid
		}
		wire, err := canonical.Bytes([]byte(item.ReceiptJCS))
		sum := sha256.Sum256(wire)
		if err != nil || !bytes.Equal(wire, []byte(item.ReceiptJCS)) || hex.EncodeToString(sum[:]) != item.Receipt.SHA256 {
			return false, v2input.ErrInvalid
		}
		var receipt AuthoredPhaseReceipt
		if decodeAuthoredParams([]byte(item.ReceiptJCS), &receipt) != nil || receipt.Ordinal != item.Ordinal || receipt.Kind != item.Kind || receipt.Instruction != item.Instruction || receipt.Tenant != inventory.Context.Tenant || receipt.Parent != inventory.Context.Parent || receipt.ProgramSHA != inventory.Context.ProgramSHA {
			return false, v2input.ErrInvalid
		}
		if item.Kind == "segment" {
			if receipt.Geometry != nil {
				return false, v2input.ErrInvalid
			}
			continue
		}
		if item.Kind != "geometry" || receipt.Geometry == nil {
			return false, v2input.ErrInvalid
		}
		var profile struct {
			Backend string `json:"backend"`
		}
		if json.Unmarshal([]byte(receipt.Geometry.Task.ProfileJCS), &profile) != nil {
			return false, v2input.ErrInvalid
		}
		switch profile.Backend {
		case "cpu":
		case "cuda", "mps":
			// Current qualified replay child protocol is explicitly upscale/1. No
			// other GPU native operation may silently execute in the CPU final lane.
			if receipt.Geometry.Task.Operation != "image.upscale" || receipt.Geometry.Task.Version != "1" {
				return false, renderUnavailable()
			}
			required = true
		default:
			return false, v2input.ErrInvalid
		}
	}
	return required, nil
}
