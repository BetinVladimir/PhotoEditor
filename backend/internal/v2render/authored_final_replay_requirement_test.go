package v2render

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func finalReplayRequirementFixture(t *testing.T, backend, operation string) (AuthoredFinalInventory, AuthoredFinalTaskPlan) {
	t.Helper()
	e, _, profile := authoredFinalTaskCompilerFixture(t)
	params := e.Params.Params
	inventory := AuthoredFinalInventory{Context: AuthoredInstructionSource{Tenant: params.TenantID, Parent: params.ParentRunID, ProgramSHA: params.ProgramSHA256}}
	inventory.Context.Instruction.Identity = params.InstructionID
	for i, ref := range params.PhaseReceipts {
		kind := "segment"
		var geometry *AuthoredGeometryPhaseWitness
		if i == 0 {
			kind = "geometry"
			nativeProfile, _ := canonical.Marshal(map[string]string{"backend": backend})
			geometry = &AuthoredGeometryPhaseWitness{Task: AuthoredPhaseTask{Operation: operation, Version: "1", ProfileJCS: string(nativeProfile)}}
		}
		receipt := AuthoredPhaseReceipt{SchemaVersion: 1, Tenant: params.TenantID, Parent: params.ParentRunID, Instruction: uuid.New(), Ordinal: i + 1, Kind: kind, ProgramSHA: params.ProgramSHA256, Geometry: geometry}
		raw, _ := canonical.Marshal(receipt)
		sha, _ := canonical.Hash(receipt)
		ref.SHA256 = sha
		params.PhaseReceipts[i] = ref
		inventory.Phases = append(inventory.Phases, AuthoredPublishedPhase{Ordinal: i + 1, Kind: kind, Instruction: receipt.Instruction, Receipt: ref, ReceiptJCS: string(raw)})
	}
	compiled, err := CompileAuthoredFinalParams(params)
	if err != nil {
		t.Fatal(err)
	}
	sum, _ := canonical.Hash(params)
	paramsRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: sum}
	roots, err := AuthoredFinalInputRoots(compiled, paramsRef)
	if err != nil {
		t.Fatal(err)
	}
	e.Params = compiled
	e.Inputs = v2draft.TaskInputEvidence{Entries: roots, Occurrences: make([]v2draft.OccurrenceWitness, len(roots))}
	raw, _ := canonical.Marshal(profile)
	plan, err := compileAuthoredFinalTask(e, paramsRef, raw)
	if err != nil {
		t.Fatal(err)
	}
	return inventory, plan
}
func TestAuthoredFinalReplayRequirementsUseCompleteImmutablePrefix(t *testing.T) {
	for _, backend := range []string{"cpu", "cuda", "mps"} {
		t.Run(backend, func(t *testing.T) {
			inventory, base := finalReplayRequirementFixture(t, backend, "image.upscale")
			required, err := authoredFinalRequiresNativeReplay(inventory, base)
			if err != nil || required != (backend != "cpu") {
				t.Fatal(required, err)
			}
		})
	}
}
func TestAuthoredFinalReplayRequirementsRejectMissingReorderedAndChangedPrefix(t *testing.T) {
	for _, change := range []string{"missing", "reordered", "bytes", "foreignreceipt", "foreignparent", "unsupportedgpu"} {
		t.Run(change, func(t *testing.T) {
			inventory, base := finalReplayRequirementFixture(t, "mps", "image.upscale")
			switch change {
			case "missing":
				inventory.Phases = inventory.Phases[:1]
			case "reordered":
				inventory.Phases[0], inventory.Phases[1] = inventory.Phases[1], inventory.Phases[0]
			case "bytes":
				inventory.Phases[0].ReceiptJCS += " "
			case "foreignreceipt":
				inventory.Phases[0].Receipt.ArtifactID = uuid.New()
			case "foreignparent":
				inventory.Context.Parent = uuid.New()
			case "unsupportedgpu":
				inventory, base = finalReplayRequirementFixture(t, "mps", "optical.lens")
			}
			if _, err := authoredFinalRequiresNativeReplay(inventory, base); err == nil {
				encoded, _ := json.Marshal(inventory)
				t.Fatal("accepted", string(encoded))
			}
		})
	}
}
