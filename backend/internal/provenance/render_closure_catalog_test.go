package provenance

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestSigningHistoricalClosureRequiresInstalledAuthority(t *testing.T) {
	if _, err := (SQLSigningRenderClosureReader{}).ReadSigningRenderClosure(context.Background(), auth.Identity{}, ReadySigningExport{}, CanonicalSigningTask{}); err == nil {
		t.Fatal("missing dependencies accepted")
	}
	if _, err := (SQLSigningRenderClosureCatalog{}).Lookup(context.Background(), auth.Identity{}, uuid.New(), v2draft.LayerRef{}); err == nil {
		t.Fatal("missing catalogue accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (SQLSigningRenderClosureReader{}).ReadSigningRenderClosure(ctx, auth.Identity{}, ReadySigningExport{}, CanonicalSigningTask{}); !errors.Is(err, context.Canceled) {
		t.Fatal("reader cancellation lost", err)
	}
	if _, err := (SQLSigningRenderClosureCatalog{}).Lookup(ctx, auth.Identity{}, uuid.New(), v2draft.LayerRef{}); !errors.Is(err, context.Canceled) {
		t.Fatal("catalogue cancellation lost", err)
	}
}

func TestSigningHistoricalLearningOccurrenceRequiresFrozenBindings(t *testing.T) {
	sha := strings.Repeat("a", 64)
	pin := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: sha} }
	learning := v2draft.FrozenRenderLearning{VersionID: uuid.New(), AnalysisID: uuid.New(), Model: pin(), Descriptor: pin(), Ordinal: 0}
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: pin(), CanvasHash: sha}
	for _, contract := range []string{"CandidateProfile", "DescriptorShard"} {
		node := v2draft.LayerRef{Contract: contract, Ref: learning.Model, ParentRef: pin(), ExpectedLearning: &learning, ExpectedBinding: &binding, ExpectedModelLockSHA: sha}
		if contract == "DescriptorShard" {
			node.Ref = learning.Descriptor
		}
		if !validSigningRenderOccurrence(node) {
			t.Fatal("valid frozen occurrence refused")
		}
		for _, mode := range []string{"learning", "binding", "parent", "ref", "ordinal", "model_lock"} {
			changed := node
			ownLearning := learning
			changed.ExpectedLearning = &ownLearning
			switch mode {
			case "learning":
				changed.ExpectedLearning = nil
			case "binding":
				changed.ExpectedBinding = nil
			case "parent":
				changed.ParentRef = v2input.Ref{}
			case "ref":
				changed.Ref = pin()
			case "ordinal":
				ownLearning.Ordinal = 5000
			case "model_lock":
				if contract != "DescriptorShard" {
					continue
				}
				changed.ExpectedModelLockSHA = "invalid"
			}
			if validSigningRenderOccurrence(changed) {
				t.Fatal("invalid occurrence accepted", contract, mode)
			}
		}
	}
}

func TestSigningHistoricalFrameRequiresSourceBinding(t *testing.T) {
	pin := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	source, descriptor := pin(), pin()
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: source, CanvasHash: strings.Repeat("b", 64)}
	frame := v2draft.RenderFrameAuthority{Root: pin(), Descriptor: descriptor, SelectedVersion: uuid.New(), Learning: v2draft.FrozenRenderLearning{Descriptor: descriptor, AnalysisID: uuid.New()}, ModelLockSHA: strings.Repeat("c", 64), Ordinal: 0}
	node := v2draft.LayerRef{Contract: "WorkingImage", Ref: source, ParentRef: descriptor, ExpectedBinding: &binding, CanvasHash: binding.CanvasHash, RenderFrame: &frame}
	if !validSigningRenderOccurrence(node) {
		t.Fatal("valid frame refused")
	}
	changed := node
	changed.ExpectedBinding = nil
	if validSigningRenderOccurrence(changed) {
		t.Fatal("missing frame binding accepted")
	}
	changed = node
	changed.Ref = pin()
	if validSigningRenderOccurrence(changed) {
		t.Fatal("foreign frame source accepted")
	}
	changed = node
	changed.CanvasHash = strings.Repeat("d", 64)
	if validSigningRenderOccurrence(changed) {
		t.Fatal("foreign canvas accepted")
	}
}

func TestSigningHistoricalBinaryLeavesRequireExactParentPin(t *testing.T) {
	pin := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	for _, contract := range []string{"WorkingPixels", "WorkingAlpha", "MaskPixels", "DenseWarpField"} {
		node := v2draft.LayerRef{Contract: contract, Ref: pin(), ParentRef: pin()}
		if !validSigningRenderOccurrence(node) {
			t.Fatal("valid binary occurrence refused", contract)
		}
		changed := node
		changed.ParentRef = v2input.Ref{}
		if validSigningRenderOccurrence(changed) {
			t.Fatal("unparented binary accepted", contract)
		}
		changed = node
		changed.ParentRef = node.Ref
		if validSigningRenderOccurrence(changed) {
			t.Fatal("self parent accepted", contract)
		}
	}
	source, descriptor := pin(), pin()
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: source, CanvasHash: strings.Repeat("b", 64)}
	frame := v2draft.RenderFrameAuthority{Root: pin(), Descriptor: descriptor, SelectedVersion: uuid.New(), Learning: v2draft.FrozenRenderLearning{Descriptor: descriptor, AnalysisID: uuid.New()}, ModelLockSHA: strings.Repeat("c", 64)}
	for _, contract := range []string{"WorkingPixels", "WorkingAlpha"} {
		node := v2draft.LayerRef{Contract: contract, Ref: pin(), ParentRef: source, ExpectedBinding: &binding, RenderFrame: &frame}
		if !validSigningRenderOccurrence(node) {
			t.Fatal("valid frame array refused")
		}
		node.ParentRef = pin()
		if validSigningRenderOccurrence(node) {
			t.Fatal("foreign frame array parent accepted")
		}
	}
}

func TestSigningHistoricalCatalogueSnapshotOwnsNestedBuffers(t *testing.T) {
	task := CanonicalSigningTask{Inputs: json.RawMessage(`[]`), Outputs: json.RawMessage(`[]`), Metrics: json.RawMessage(`{}`)}
	assume := "srgb"
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New()}
	learning := v2draft.FrozenRenderLearning{VersionID: uuid.New()}
	background := v2draft.FrozenBackgroundAsset{AssumeProfile: &assume}
	frame := v2draft.RenderFrameAuthority{SelectedVersion: uuid.New()}
	node := v2draft.LayerRef{ExpectedBinding: &binding, ExpectedLearning: &learning, ExpectedBackground: &background, RenderFrame: &frame}
	ownedTask, ownedNode, comparison, err := ownSigningRenderCatalogSnapshot(task, node)
	if err != nil {
		t.Fatal(err)
	}
	task.Inputs[0] = 'x'
	task.Outputs[0] = 'x'
	task.Metrics[0] = 'x'
	binding.AssetVersionID = uuid.Nil
	learning.VersionID = uuid.Nil
	assume = "changed"
	frame.SelectedVersion = uuid.Nil
	if string(ownedTask.Inputs) != "[]" || string(ownedTask.Outputs) != "[]" || string(ownedTask.Metrics) != "{}" || ownedNode.ExpectedBinding.AssetVersionID == uuid.Nil || ownedNode.ExpectedLearning.VersionID == uuid.Nil || *ownedNode.ExpectedBackground.AssumeProfile != "srgb" || ownedNode.RenderFrame.SelectedVersion == uuid.Nil {
		t.Fatal("snapshot aliases original authority")
	}
	comparison[0] = 'x'
	if string(ownedTask.Inputs) != "[]" {
		t.Fatal("owned task aliases comparison buffer")
	}
	ownedNode.ExpectedBinding.AssetVersionID = uuid.New()
	if binding.AssetVersionID != uuid.Nil {
		t.Fatal("snapshot mutates original binding")
	}
}
func TestSigningHistoricalCatalogueSnapshotRefusesMalformedJSON(t *testing.T) {
	if _, _, _, err := ownSigningRenderCatalogSnapshot(CanonicalSigningTask{Inputs: json.RawMessage(`invalid`)}, v2draft.LayerRef{}); err == nil {
		t.Fatal("malformed task JSON accepted")
	}
}
