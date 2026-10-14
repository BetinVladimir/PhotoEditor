package provenance

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestSigningProducerTaskComparisonOwnsCanonicalEvidence(t *testing.T) {
	sha := strings.Repeat("a", 64)
	task := CanonicalSigningTask{Task: uuid.New(), Run: uuid.New(), Result: uuid.New(), Attempt: 1, Fence: 1, Operation: "asset.decode", Version: "1", InputDigest: sha, ResultDigest: sha, Params: v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}, Inputs: json.RawMessage(`[]`), Outputs: json.RawMessage(`[]`), Metrics: json.RawMessage(`{}`)}
	owned, err := ownSigningProducerTask(task)
	if err != nil {
		t.Fatal(err)
	}
	if !sameSigningProducerTask(owned, task) {
		t.Fatal("same task refused")
	}
	changed := task
	changed.Fence++
	if sameSigningProducerTask(owned, changed) {
		t.Fatal("changed fence accepted")
	}
	changed = task
	changed.Result = uuid.New()
	if sameSigningProducerTask(owned, changed) {
		t.Fatal("changed result accepted")
	}
	task.Inputs[0] = 'x'
	if string(owned.Inputs) != "[]" || sameSigningProducerTask(owned, task) {
		t.Fatal("mutable task alias accepted")
	}
}
