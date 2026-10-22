package v2render

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

// Rebind exact typed output digests after a fixture-only mutation. No producer,
// qualification or persisted review is fabricated by this content unit test.
func authoredCandidateBoundaryDocuments(t *testing.T, raw [][]byte, o []AuthoredFaceProducerObservation, empty bool, mutation string) {
	t.Helper()
	docs := make([]map[string]any, 3)
	for i := range docs {
		if err := json.Unmarshal(raw[i+1], &docs[i]); err != nil {
			t.Fatal(err)
		}
	}
	if empty {
		docs[0]["faces"] = []any{}
		docs[0]["accepted_count"] = 0
		docs[0]["candidate_count"] = 0
		docs[0]["warnings"] = []string{"no_faces"}
		docs[1]["landmarks"] = []any{}
		docs[1]["mean_confidence"] = 0
		docs[1]["warnings"] = []string{"no_faces"}
		docs[2]["parsed_faces"] = 0
		docs[2]["mean_confidence"] = 0
		docs[2]["warnings"] = []string{"no_faces"}
	}
	if mutation == "object" {
		docs[1]["landmarks"].([]any)[0].(map[string]any)["object_id"] = uuid.New().String()
	}
	for i := range docs {
		if i > 0 {
			docs[i]["faces"] = o[0].Roots[0]
		}
		if mutation == "parser-link" && i == 2 {
			docs[i]["faces"] = v2input.Ref{ArtifactID: uuid.New(), SHA256: o[0].Roots[0].SHA256}
		}
		if mutation == "empty-warning" && i == 1 {
			docs[i]["warnings"] = []string{}
		}
		body, err := canonical.Marshal(docs[i])
		if err != nil {
			t.Fatal(err)
		}
		var observation struct {
			Warnings []json.RawMessage `json:"warnings"`
		}
		if json.Unmarshal(body, &observation) != nil {
			t.Fatal("warning metadata")
		}
		o[i].ReviewRequired = len(observation.Warnings) > 0
		o[i].RunState = "completed"
		if o[i].ReviewRequired {
			o[i].RunState = "waiting_review"
		}
		raw[i+1] = body
		digest, err := canonical.Hash(json.RawMessage(body))
		if err != nil {
			t.Fatal(err)
		}
		o[i].Roots[len(o[i].Roots)-1].SHA256 = digest
	}
}

func TestAuthoredCurrentFaceCandidateRetainsExplicitZeroFaceObservation(t *testing.T) {
	source, raw, o := authoredFaceCandidateFixture(t)
	authoredCandidateBoundaryDocuments(t, raw, o, true, "")
	candidate, body, err := CompileAuthoredFaceSelectionCandidate(source, raw[0], raw[1], raw[2], raw[3], o[0], o[1], o[2])
	if err != nil || candidate.Objects == nil || len(candidate.Objects) != 0 || len(body) == 0 {
		t.Fatal("valid zero-face observation lost", err)
	}
}

func TestAuthoredCurrentFaceCandidateRefusesSameCountForeignObjectAndFalseEmptyObservation(t *testing.T) {
	for _, name := range []string{"object", "parser-link", "empty-warning"} {
		t.Run(name, func(t *testing.T) {
			source, raw, o := authoredFaceCandidateFixture(t)
			authoredCandidateBoundaryDocuments(t, raw, o, name == "empty-warning", name)
			if _, _, err := CompileAuthoredFaceSelectionCandidate(source, raw[0], raw[1], raw[2], raw[3], o[0], o[1], o[2]); err == nil {
				t.Fatal("invalid linked observation accepted")
			}
		})
	}
}
