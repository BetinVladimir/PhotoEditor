package provenance

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestSigningRenderParentsOwnNestedCatalogBuffers(t *testing.T) {
	original := []SigningRenderParent{{Root: v2input.Ref{ArtifactID: uuid.New(), SHA256: "original"}, Producer: CanonicalSigningTask{Task: uuid.New(), Inputs: json.RawMessage(`[]`), Outputs: json.RawMessage(`[]`), Metrics: json.RawMessage(`{}`)}}}
	owned, raw, err := ownSigningRenderParents(original)
	if err != nil {
		t.Fatal(err)
	}
	original[0].Root.SHA256 = "changed"
	original[0].Producer.Inputs[0] = 'x'
	original[0].Producer.Outputs[0] = 'x'
	original[0].Producer.Metrics[0] = 'x'
	if owned[0].Root.SHA256 != "original" || string(owned[0].Producer.Inputs) != "[]" || string(owned[0].Producer.Outputs) != "[]" || string(owned[0].Producer.Metrics) != "{}" {
		t.Fatal("parent snapshot aliases adapter state")
	}
	raw[0] = 'x'
	if string(owned[0].Producer.Inputs) != "[]" {
		t.Fatal("snapshot aliases canonical comparison bytes")
	}
}
func TestSigningRenderParentsRefuseInvalidCatalogueShape(t *testing.T) {
	for _, parents := range [][]SigningRenderParent{nil, make([]SigningRenderParent, 34), {{Producer: CanonicalSigningTask{Inputs: json.RawMessage(`invalid`)}}}} {
		if _, _, err := ownSigningRenderParents(parents); err == nil {
			t.Fatal("invalid catalogue accepted")
		}
	}
	owned, _, err := ownSigningRenderParents([]SigningRenderParent{})
	if err != nil || owned == nil || len(owned) != 0 {
		t.Fatal("empty explicit parent list refused", err)
	}
}
