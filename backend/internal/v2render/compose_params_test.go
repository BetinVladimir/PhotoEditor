package v2render

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func compositionInputs() (Layer, v2input.SessionVersionBinding, v2input.Ref) {
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	base := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(), CanvasHash: strings.Repeat("b", 64)}
	mask, _ := canonical.Marshal(map[string]any{"op": "ref", "mask": ref(), "canvas_hash": base.CanvasHash})
	return Layer{ID: uuid.New(), Enabled: true, Opacity: .5, Blend: "screen", Operation: "adjust.exposure", OperationVersion: "1", Params: ref(), Mask: mask}, base, ref()
}

func TestCompileCompositionExactNullableContractAndOwnership(t *testing.T) {
	layer, base, proposal := compositionInputs()
	compiled, err := CompileComposition(layer, base, nil, proposal, nil, 257, 2)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(compiled.JSON, &fields) != nil || len(fields) != 8 || string(fields["source_result"]) != "null" || string(fields["protected_mask"]) != "null" {
		t.Fatal("nullable wire contract", string(compiled.JSON))
	}
	want, _ := LayerResources(257, 2, 1, 1, 512)
	if compiled.Budget.Resources != want || compiled.Params.Base != base || compiled.Params.ProposalResult != proposal {
		t.Fatal("lost composition inputs")
	}
	before := string(compiled.Params.Mask)
	layer.Mask[0] = ' '
	if string(compiled.Params.Mask) != before {
		t.Fatal("mask aliases caller")
	}
	layer, base, proposal = compositionInputs()
	parent := proposal
	protected := proposal
	withRefs, err := CompileComposition(layer, base, &parent, proposal, &protected, 257, 2)
	if err != nil {
		t.Fatal(err)
	}
	parent.SHA256 = strings.Repeat("c", 64)
	protected.SHA256 = strings.Repeat("d", 64)
	if withRefs.Params.SourceResult.SHA256 != proposal.SHA256 || withRefs.Params.ProtectedMask.SHA256 != proposal.SHA256 {
		t.Fatal("parent aliases caller")
	}
}

func TestCompileCompositionRejectsInactiveWrongCanvasAndInvalidParent(t *testing.T) {
	layer, base, proposal := compositionInputs()
	layer.Enabled = false
	if _, err := CompileComposition(layer, base, nil, proposal, nil, 2, 1); err == nil {
		t.Fatal("inactive")
	}
	layer.Enabled = true
	base.CanvasHash = strings.Repeat("f", 64)
	if _, err := CompileComposition(layer, base, nil, proposal, nil, 2, 1); err == nil {
		t.Fatal("mask canvas mismatch")
	}
	layer, base, proposal = compositionInputs()
	bad := v2input.Ref{}
	if _, err := CompileComposition(layer, base, &bad, proposal, nil, 2, 1); err == nil {
		t.Fatal("invalid parent")
	}
}
