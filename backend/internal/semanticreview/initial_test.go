package semanticreview

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestInitialTargetsPreservePythonPlanStatesAndExactCandidateSources(t *testing.T) {
	raw, err := os.ReadFile("../v2input/testdata/semantic-plan-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Plan      json.RawMessage                `json:"plan"`
		Params    v2input.SemanticAssembleParams `json:"params"`
		Snapshot  json.RawMessage                `json:"snapshot"`
		Documents []struct {
			Ref  v2input.Ref     `json:"ref"`
			Body json.RawMessage `json:"body"`
		} `json:"documents"`
	}
	if json.Unmarshal(raw, &fixture) != nil {
		t.Fatal("fixture")
	}
	ref := func(body []byte) v2input.Ref {
		sum := sha256.Sum256(body)
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	}
	documents := make([]v2input.SemanticCandidateDocument, len(fixture.Documents))
	for n, d := range fixture.Documents {
		documents[n] = v2input.SemanticCandidateDocument{Ref: d.Ref, Body: d.Body}
	}
	planRef, snapshotRef := ref(fixture.Plan), ref(fixture.Snapshot)
	rows, err := CompileInitialTargets(planRef, fixture.Plan, fixture.Params, snapshotRef, fixture.Snapshot, documents)
	if err != nil || len(rows) != 3 {
		t.Fatal(rows, err)
	}
	for n, state := range []string{"unsupported", "proposed", "rejected"} {
		if rows[n].State != state || rows[n].CandidateArtifactID != documents[2-n].Ref.ArtifactID {
			t.Fatal(rows)
		}
	}
	documents[0].Ref.ArtifactID = uuid.New()
	if _, err := CompileInitialTargets(planRef, fixture.Plan, fixture.Params, snapshotRef, fixture.Snapshot, documents); err == nil {
		t.Fatal("source substitution accepted")
	}
}
