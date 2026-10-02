package v2render

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestCompositionDraftBindsIdentityInputsAndComputedResources(t *testing.T) {
	layer, base, proposal := compositionInputs()
	compiled, err := CompileComposition(layer, base, nil, proposal, nil, 257, 2)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(compiled.JSON)
	workflow := uuid.New()
	task, _ := v2input.TaskID(workflow, "layer.compose", 7)
	request := CompositionPreparation{Tenant: uuid.New(), Task: task, Actor: auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, Layer: layer, Base: base, ProposalResult: proposal, Width: 257, Height: 2, ParamsRef: v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}}
	roots, err := CompositionInputRoots(compiled, request.ParamsRef)
	if err != nil {
		t.Fatal(err)
	}
	draft, frozen, err := CompileCompositionDraft(workflow, 7, request, roots)
	if err != nil || draft.ID != task || draft.NodeKey != "layer.compose:7" || draft.Operation != "layer.compose" || draft.OperationVersion != "1" || draft.ResourceClass != "cpu" || draft.ParamsArtifactID != request.ParamsRef.ArtifactID || draft.ParamsSHA != request.ParamsRef.SHA256 || draft.InputDigest != frozen.InputDigest {
		t.Fatal(draft, err)
	}
	var profile struct {
		Backend   string                `json:"backend"`
		Renderer  string                `json:"renderer_version"`
		Resources LayerResourceEstimate `json:"resource_estimate"`
	}
	if err := json.Unmarshal(draft.ExecutionProfile, &profile); err != nil || profile.Backend != "cpu" || profile.Renderer != "v2.2" || profile.Resources != compiled.Budget.Resources || profile.Resources.VRAMBytes != 0 {
		t.Fatal("computed profile", profile, err)
	}
	// Frozen cursor owns its entries; changing the caller's input cannot alter
	// registration retries after the shell's digest is fixed.
	roots[0].Ref.ArtifactID = uuid.New()
	mutatedID := roots[0].Ref.ArtifactID
	cursor, err := frozen.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for {
		entry, ok, err := cursor.Next(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		if entry.Ref.ArtifactID == mutatedID {
			t.Fatal("caller mutation changed frozen inventory")
		}
		count++
	}
	if count < 4 {
		t.Fatal("missing frozen roots")
	}
	request.Task = uuid.New()
	if _, _, err := CompileCompositionDraft(workflow, 7, request, roots); err == nil {
		t.Fatal("foreign task identity accepted")
	}
	request.Task = task
	if _, _, err := CompileCompositionDraft(workflow, -1, request, roots); err == nil {
		t.Fatal("invalid ordinal accepted")
	}
}
