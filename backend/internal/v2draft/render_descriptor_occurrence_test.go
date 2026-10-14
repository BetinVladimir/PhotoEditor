package v2draft

import (
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestRenderDescriptorOccurrenceRequiresSelectedOrdinalSource(t *testing.T) {
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref, CanvasHash: strings.Repeat("b", 64)}
	learning := FrozenRenderLearning{VersionID: uuid.New(), Model: ref, Descriptor: ref, AnalysisID: uuid.New(), Ordinal: 7}
	node := LayerRef{Ref: ref, Contract: "DescriptorShard", ExpectedBinding: &binding, ExpectedLearning: &learning}
	frame := func(ordinal int, b v2input.SessionVersionBinding) any {
		return map[string]any{"ordinal": ordinal, "binding": b}
	}
	wrong := binding
	wrong.AssetVersionID = uuid.New()
	for i, frames := range [][]any{{frame(7, binding)}, {frame(6, binding)}, {frame(7, wrong), frame(8, binding)}, {frame(7, binding), frame(7, binding)}} {
		body, _ := canonical.Marshal(map[string]any{"frames": frames})
		err := validateRenderDescriptorOccurrence(body, node)
		if (i == 0) != (err == nil) {
			t.Fatal(i, err)
		}
	}
}

func TestDocumentOccurrenceChildrenInheritsDetachedSourceAndFrame(t *testing.T) {
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	childRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref}
	frame := RenderFrameAuthority{Root: ref, Ordinal: 9}
	body, err := canonical.Marshal(map[string]any{"child": childRef})
	if err != nil {
		t.Fatal(err)
	}
	node := LayerRef{Ref: ref, Contract: "Params", Path: "TaskInputs[0].nested", ExpectedBinding: &binding, RenderFrame: &frame}
	children, err := DocumentOccurrenceChildren(body, node)
	if err != nil || len(children) != 1 {
		t.Fatal(children, err)
	}
	child := children[0]
	if child.ParentRef != ref || child.Ref != childRef || child.Path != node.Path+".child" || child.ExpectedBinding == nil || *child.ExpectedBinding != binding || child.RenderFrame == nil || *child.RenderFrame != frame {
		t.Fatal("derived context differs", child)
	}
	binding.AssetVersionID = uuid.New()
	frame.Ordinal = 10
	if child.ExpectedBinding.AssetVersionID == binding.AssetVersionID || child.RenderFrame.Ordinal == frame.Ordinal {
		t.Fatal("derived child aliases parent context")
	}
}

func TestDocumentOccurrenceChildrenWorkingImageCanvas(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if err = json.Unmarshal(raw, &images); err != nil || len(images) == 0 {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(images[0])
	if err != nil {
		t.Fatal(err)
	}
	hash, err := canonical.Hash(image.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	node := LayerRef{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, Path: "frame.source", Contract: "WorkingImage", CanvasHash: hash}
	if _, err := DocumentOccurrenceChildren(images[0], node); err != nil {
		t.Fatal("correct image canvas rejected", err)
	}
	node.CanvasHash = strings.Repeat("b", 64)
	if _, err := DocumentOccurrenceChildren(images[0], node); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("foreign declared canvas accepted", err)
	}
}
