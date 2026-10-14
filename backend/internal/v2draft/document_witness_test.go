package v2draft

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestOccurrenceNodeOwnsFrameLearningAndSourcePointers(t *testing.T) {
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New()}
	learning := FrozenRenderLearning{VersionID: uuid.New()}
	frame := RenderFrameAuthority{SelectedVersion: uuid.New(), Learning: learning}
	node := LayerRef{ExpectedBinding: &binding, ExpectedLearning: &learning, RenderFrame: &frame}
	owned := ownOccurrenceNode(node)
	binding.AssetVersionID = uuid.New()
	learning.VersionID = uuid.New()
	frame.SelectedVersion = uuid.New()
	frame.Learning.VersionID = uuid.New()
	if owned.ExpectedBinding.AssetVersionID == binding.AssetVersionID || owned.ExpectedLearning.VersionID == learning.VersionID || owned.RenderFrame.SelectedVersion == frame.SelectedVersion || owned.RenderFrame.Learning.VersionID == frame.Learning.VersionID {
		t.Fatal("occurrence evidence aliases mutable authority pointers")
	}
}

func TestOccurrenceNodeOwnsManualRasterContext(t *testing.T) {
	original := ManualRasterVectors{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, Contract: "ManualMask"}
	expected := original
	owned := ownOccurrenceNode(LayerRef{ManualVectors: &original})
	original.Ref.ArtifactID = uuid.New()
	original.Ref.SHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	original.Contract = "Params"
	if owned.ManualVectors == &original || *owned.ManualVectors != expected {
		t.Fatal("manual context aliases caller state")
	}
	second := ownOccurrenceNode(owned)
	owned.ManualVectors.Contract = "Artifact"
	if *second.ManualVectors != expected {
		t.Fatal("repeated occurrence aliases previous evidence")
	}
	if ownOccurrenceNode(LayerRef{}).ManualVectors != nil {
		t.Fatal("missing context manufactured")
	}
}
