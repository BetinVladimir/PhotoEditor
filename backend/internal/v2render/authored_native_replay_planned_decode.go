package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func decodeAuthoredReplayPlannedInventory(ctx context.Context, raw []byte, r v2input.AuthoredNativeReplayRequest, who auth.Identity) (AuthoredFinalInventory, error) {
	empty := AuthoredFinalInventory{}
	fail := func() (AuthoredFinalInventory, error) { return empty, v2input.ErrInvalid }
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	var out AuthoredFinalInventory
	if len(raw) < 2 || len(raw) > 64<<20 || decodeAuthoredParams(raw, &out) != nil {
		return fail()
	}
	source := out.Context
	tenant := r.Consumer.Tenant
	parent := r.Consumer.Parent
	ordinal := r.Consumer.Ordinal
	if source.Tenant != tenant || source.Parent != parent || source.Actor != who.UserID || source.Session != who.SessionID || source.Instruction.Ordinal != ordinal || source.Instruction.Identity != r.Consumer.Instruction || source.ProgramSHA != r.Consumer.ProgramSHA || source.PredecessorSHA != r.Consumer.PredecessorSHA || source.Source != r.Consumer.Source || len(out.Phases) != ordinal-1 {
		return fail()
	}
	if r.Consumer.Operation != "render.compose" && r.Consumer.Operation != "render.authored_geometry" {
		return fail()
	}
	if (r.Consumer.Operation == "render.compose" && source.Instruction.Kind != "finalize") || (r.Consumer.Operation == "render.authored_geometry" && source.Instruction.Kind != "geometry") {
		return fail()
	}
	verify := func(ref v2input.Ref, body string) bool {
		if ref.ArtifactID == uuid.Nil || len(body) < 2 || len(body) > 8<<20 {
			return false
		}
		digest := sha256.Sum256([]byte(body))
		wire, e := canonical.Bytes([]byte(body))
		return e == nil && bytes.Equal(wire, []byte(body)) && hex.EncodeToString(digest[:]) == ref.SHA256
	}
	if !verify(out.BaseReceipt, out.BaseReceiptJCS) {
		return fail()
	}
	var base struct {
		Tenant    uuid.UUID                     `json:"tenant_id"`
		Parent    uuid.UUID                     `json:"parent_run_id"`
		Original  v2input.Ref                   `json:"original_recipe"`
		Processed v2input.SessionVersionBinding `json:"processed_source"`
	}
	if json.Unmarshal([]byte(out.BaseReceiptJCS), &base) != nil || base.Tenant != tenant || base.Parent != parent || base.Original != source.OriginalRecipe || !authoredBindingValid(base.Processed) {
		return fail()
	}
	previous := out.BaseReceipt.SHA256
	current := base.Processed
	seen := map[uuid.UUID]bool{out.BaseReceipt.ArtifactID: true}
	for i, item := range out.Phases {
		if item.Ordinal != i+1 || item.Instruction == uuid.Nil || seen[item.Receipt.ArtifactID] || !verify(item.Receipt, item.ReceiptJCS) {
			return fail()
		}
		var phase AuthoredPhaseReceipt
		if decodeAuthoredParams([]byte(item.ReceiptJCS), &phase) != nil {
			return fail()
		}
		if phase.Tenant != tenant || phase.Parent != parent || phase.Ordinal != item.Ordinal || phase.Kind != item.Kind || phase.Instruction != item.Instruction || phase.InputSource != current || phase.PredecessorSHA != previous || phase.ProgramSHA != source.ProgramSHA || phase.OriginalRecipe != source.OriginalRecipe {
			return fail()
		}
		if phase.Kind != "segment" && phase.Kind != "geometry" {
			return fail()
		}
		bound := source
		bound.Source = current
		bound.PredecessorSHA = previous
		bound.Instruction.Ordinal = phase.Ordinal
		bound.Instruction.Identity = phase.Instruction
		bound.Instruction.Kind = phase.Kind
		body, sha, err := CompileAuthoredPhaseReceipt(bound, phase.AuthoredPhaseTask, phase.OutputSource, phase.Result, phase.Report, phase.Geometry, phase.PhysicalClosure)
		if err != nil || sha != item.Receipt.SHA256 || !bytes.Equal(body, []byte(item.ReceiptJCS)) {
			return fail()
		}
		current = phase.OutputSource
		previous = item.Receipt.SHA256
		seen[item.Receipt.ArtifactID] = true
	}
	if current != source.Source || previous != source.PredecessorSHA {
		return fail()
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return out, nil
}
