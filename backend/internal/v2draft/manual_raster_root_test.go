package v2draft

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestManualRootVectorsRequireExplicitOriginalContext(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/face-manual-root-python.json")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	parent := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	root, err := review.ParseManualRasterDocument(raw, review.ArtifactRef{ID: parent.ArtifactID, SHA256: parent.SHA256})
	if err != nil {
		t.Fatal(err)
	}
	vector := v2input.Ref{ArtifactID: root.Vectors.ID, SHA256: root.Vectors.SHA256}
	for _, contract := range []string{"Params", "ManualMask"} {
		children, err := manualRasterRootRefs(raw, "Root", parent, &ManualRasterVectors{Ref: vector, Contract: contract})
		if err != nil || len(children) != 6 || children[1].Contract != contract || children[1].Ref != vector {
			t.Fatal(children, err)
		}
		occurrence, err := DocumentOccurrenceChildren(raw, LayerRef{Ref: parent, Contract: "RasterizedMask", Path: "Task.mask", ManualVectors: &ManualRasterVectors{Ref: vector, Contract: contract}})
		if err != nil || len(occurrence) != 6 {
			t.Fatal(occurrence, err)
		}
		for i, child := range occurrence {
			if child.Path != "Task.mask"+children[i].Path[len("Root"):] || child.Ref != children[i].Ref || child.Contract != children[i].Contract || child.ParentRef != parent {
				t.Fatal("occurrence traversal differs", child)
			}
		}
		if children[0].Contract != "WorkingImage" || children[2].Contract != "MaskPixels" || children[3].Contract != "MaskPixels" {
			t.Fatal("typed roots lost")
		}
		for _, child := range children {
			if child.ParentRef != parent {
				t.Fatal("parent occurrence lost")
			}
		}
	}
	for _, origin := range []*ManualRasterVectors{nil, {Ref: vector, Contract: "Artifact"}, {Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: vector.SHA256}, Contract: "Params"}} {
		if _, err := DocumentOccurrenceChildren(raw, LayerRef{Ref: parent, Contract: "RasterizedMask", Path: "Task.mask", ManualVectors: origin}); err == nil {
			t.Fatal("unproven occurrence accepted")
		}
		if _, err := manualRasterRootRefs(raw, "Root", parent, origin); err == nil {
			t.Fatal("unproven vector context accepted")
		}
	}
}
