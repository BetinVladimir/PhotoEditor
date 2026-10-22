package v2input

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
)

func nativeEvidenceFixture(t *testing.T) AuthoredNativeReplayEvidence {
	t.Helper()
	ref := func() Ref { return Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	profile := `{"backend":"cuda"}`
	sha, err := canonical.Hash(map[string]string{"backend": "cuda"})
	if err != nil {
		t.Fatal(err)
	}
	root, report, old, new := ref(), ref(), ref(), ref()
	request := ref()
	return AuthoredNativeReplayEvidence{SchemaVersion: 1, Request: request, RequestSHA: request.SHA256, Replay: AuthoredNativeObservation{Task: AuthoredNativeProducer{Run: uuid.New(), Task: uuid.New(), Result: uuid.New(), Operation: "image.upscale", Version: "1", Attempt: 1, Fence: 2, Params: ref(), InputDigest: strings.Repeat("b", 64), ProfileJCS: profile, ProfileSHA: sha, Outputs: []Ref{root, report, new}}, Root: root, Image: root, Report: report}, Comparisons: []AuthoredNativeReplayComparison{{Role: "pixels", Original: old, Replay: new, SHA: old.SHA256, Bytes: 129}}}
}
func TestAuthoredNativeReplayEvidenceTypedBinaryEdges(t *testing.T) {
	e := nativeEvidenceFixture(t)
	raw, _ := canonical.Marshal(e)
	edges, err := AuthoredNativeReplayEvidenceChildren(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges.Typed) != 7 || edges.Typed[0].Contract != "AuthoredNativeReplayRequest" || edges.Typed[5].Contract != "NpyArray" || edges.Typed[6].Ref != e.Comparisons[0].Replay {
		t.Fatal(edges)
	}
	if len(edges.Opaque) != 3 {
		t.Fatal(edges)
	}
	e.Replay.Task.Outputs[0] = Ref{}
	if !validRef(edges.Opaque[0]) {
		t.Fatal("edge storage aliased decoded source")
	}
}
func TestAuthoredNativeReplayEvidenceMalformedAndConflictingRefsRefused(t *testing.T) {
	cases := map[string]func(*AuthoredNativeReplayEvidence){"schema": func(e *AuthoredNativeReplayEvidence) { e.SchemaVersion = 2 }, "request sha": func(e *AuthoredNativeReplayEvidence) { e.RequestSHA = strings.Repeat("c", 64) }, "zero task": func(e *AuthoredNativeReplayEvidence) { e.Replay.Task.Task = uuid.Nil }, "duplicate role": func(e *AuthoredNativeReplayEvidence) { e.Comparisons = append(e.Comparisons, e.Comparisons[0]) }, "same id": func(e *AuthoredNativeReplayEvidence) { e.Comparisons[0].Replay = e.Comparisons[0].Original }, "zero bytes": func(e *AuthoredNativeReplayEvidence) { e.Comparisons[0].Bytes = 0 }, "overflow": func(e *AuthoredNativeReplayEvidence) { e.Comparisons[0].Bytes = 9007199254740992 }, "foreign digest": func(e *AuthoredNativeReplayEvidence) { e.Comparisons[0].SHA = strings.Repeat("c", 64) }, "typed conflict": func(e *AuthoredNativeReplayEvidence) { e.Comparisons[0].Original = e.Replay.Root }}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			e := nativeEvidenceFixture(t)
			mutate(&e)
			raw, _ := canonical.Marshal(e)
			if _, err := AuthoredNativeReplayEvidenceChildren(raw); err == nil {
				t.Fatal("hostile evidence accepted")
			}
		})
	}
}
func TestAuthoredNativeReplayEvidenceClosedCanonicalWire(t *testing.T) {
	e := nativeEvidenceFixture(t)
	raw, _ := canonical.Marshal(e)
	for _, wire := range [][]byte{append([]byte(" "), raw...), []byte(strings.Replace(string(raw), `"schema_version":1`, `"schema_version":1,"extra":{}`, 1)), []byte(strings.Replace(string(raw), `"attempt":1`, `"attempt":true`, 1)), []byte(strings.Replace(string(raw), `"bytes":129`, `"bytes":129.5`, 1))} {
		if _, err := DecodeAuthoredNativeReplayEvidence(wire); err == nil {
			t.Fatal("noncanonical/unknown/invalid wire accepted")
		}
	}
}
