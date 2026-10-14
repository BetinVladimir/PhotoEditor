package semanticproposal

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

func TestDefectDescriptorSnapshotOwnsDependencyArraysAndNestedPointers(t *testing.T) {
	params := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	root := review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("b", 64)}
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New()}
	originalVersion := binding.AssetVersionID
	published := PublishedDefectParams{Ref: params, Inputs: []review.ArtifactRef{root}, ManualRoots: []review.ArtifactRef{root}}
	nodes := []v2draft.LayerRef{{Ref: params, Contract: "Params"}, {Ref: v2input.Ref{ArtifactID: root.ID, SHA256: root.SHA256}, Contract: "MaskRef", ExpectedBinding: &binding}}
	owned, err := snapshotDefectDescriptors(published, nodes)
	if err != nil {
		t.Fatal(err)
	}
	published.Inputs[0].SHA256 = strings.Repeat("c", 64)
	published.ManualRoots[0].ID = uuid.New()
	nodes[1].Contract = "Changed"
	binding.AssetVersionID = uuid.New()
	if owned.Published.Inputs[0] != root || owned.Published.ManualRoots[0] != root || owned.Nodes[1].Contract != "MaskRef" || owned.Nodes[1].ExpectedBinding.AssetVersionID != originalVersion {
		t.Fatal("caller mutation changed descriptor snapshot")
	}
	owned.Nodes[1].ExpectedBinding.AssetVersionID = uuid.New()
	if owned.Nodes[1].ExpectedBinding == nodes[1].ExpectedBinding || binding.AssetVersionID == owned.Nodes[1].ExpectedBinding.AssetVersionID {
		t.Fatal("owned descriptor retained caller pointer")
	}
}
