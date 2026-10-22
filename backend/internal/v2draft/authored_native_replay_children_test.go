package v2draft

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func replayEvidenceDocument(t *testing.T) ([]byte, v2input.Ref) {
	t.Helper()
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	root, report, pixels := ref(), ref(), ref()
	profile := `{"backend":"cuda"}`
	sha, _ := canonical.Hash(map[string]string{"backend": "cuda"})
	r := ref()
	e := v2input.AuthoredNativeReplayEvidence{SchemaVersion: 1, Request: r, RequestSHA: r.SHA256, Replay: v2input.AuthoredNativeObservation{Task: v2input.AuthoredNativeProducer{Run: uuid.New(), Task: uuid.New(), Result: uuid.New(), Operation: "image.upscale", Version: "1", Attempt: 1, Fence: 2, Params: ref(), InputDigest: strings.Repeat("b", 64), ProfileJCS: profile, ProfileSHA: sha, Outputs: []v2input.Ref{root, report, pixels}}, Root: root, Image: root, Report: report}, Comparisons: []v2input.AuthoredNativeReplayComparison{{Role: "pixels", Original: ref(), Replay: pixels, SHA: pixels.SHA256, Bytes: 64}}}
	raw, err := canonical.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	parent := ref()
	parent.SHA256, _ = canonical.Hash(e)
	return raw, parent
}
func TestAuthoredNativeReplayEvidenceDocumentChildrenAreExplicit(t *testing.T) {
	raw, parent := replayEvidenceDocument(t)
	edges, err := DocumentChildren(raw, "AuthoredNativeReplayEvidence", parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 10 || edges[0].Contract != "AuthoredNativeReplayRequest" || edges[5].Contract != "WorkingPixels" || edges[9].Contract != "Artifact" {
		t.Fatal(edges)
	}
	for _, e := range edges {
		if e.ParentRef != parent {
			t.Fatal("lost occurrence parent")
		}
	}
}
func TestAuthoredNativeReplayEvidenceNeverDiscoversRefsInsideProfileJCS(t *testing.T) {
	raw, parent := replayEvidenceDocument(t)
	var e v2input.AuthoredNativeReplayEvidence
	// Decoder validates profile as canonical JSON but treats its contents as scalar.
	if json.Unmarshal(raw, &e) != nil {
		t.Fatal("fixture")
	}
	embedded := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("f", 64)}
	profile := map[string]any{"backend": "cuda", "opaque": embedded}
	body, _ := canonical.Marshal(profile)
	e.Replay.Task.ProfileJCS = string(body)
	e.Replay.Task.ProfileSHA, _ = canonical.Hash(profile)
	raw, _ = canonical.Marshal(e)
	parent.SHA256, _ = canonical.Hash(e)
	edges, err := DocumentChildren(raw, "AuthoredNativeReplayEvidence", parent)
	if err != nil {
		t.Fatal(err)
	}
	for _, edge := range edges {
		if edge.Ref == embedded {
			t.Fatal("traversed scalar JCS")
		}
	}
}
