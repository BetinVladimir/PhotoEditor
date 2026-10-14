package v2portrait

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestRelightSubmissionRequestBoundary(t *testing.T) {
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	p := RelightParams{SchemaVersion: 1, Base: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(), CanvasHash: strings.Repeat("a", 64)}, Depth: ref(), Subject: ref(), Protected: ref(), Calibration: ref(), Lights: []RelightLight{{Radius: .5}}, Strength: .5}
	compiled, _, e := p.Compile()
	if e != nil {
		t.Fatal(e)
	}
	draftRaw, _ := canonical.Marshal(relightDraftIntentWire{SchemaVersion: 1, Snapshot: uuid.New(), SubjectKind: "manual", Params: compiled})
	draft, e := DecodeRelightDraftIntent(draftRaw)
	if e != nil || !bytes.Equal(draft.ParamsRaw, compiled) {
		t.Fatal("authored draft", e)
	}
	for _, field := range []string{"run_id", "workflow_id", "tenant_id", "pin", "capacity", "auth_session_id"} {
		var fields map[string]any
		json.Unmarshal(draftRaw, &fields)
		fields[field] = uuid.NewString()
		bad, _ := canonical.Marshal(fields)
		if _, e := DecodeRelightDraftIntent(bad); e == nil {
			t.Fatal("draft injected field", field)
		}
	}
	for _, bad := range [][]byte{[]byte("null"), append(append([]byte(nil), draftRaw...), []byte("{}")...), bytes.Replace(draftRaw, []byte(`"schema_version":1`), []byte(`"schema_version":1,"schema_version":1`), 1)} {
		if _, e := DecodeRelightDraftIntent(bad); e == nil {
			t.Fatal("invalid authored draft")
		}
	}
	w := relightSubmissionWire{SchemaVersion: 1, Snapshot: uuid.New(), Run: uuid.New(), Workflow: uuid.New(), SubjectKind: "manual", Params: compiled}
	raw, _ := canonical.Marshal(w)
	got, e := DecodeRelightSubmissionRequest(raw)
	if e != nil || !bytes.Equal(got.ParamsRaw, compiled) {
		t.Fatal("valid request", e)
	}
	original := append([]byte(nil), raw...)
	raw[0] = 'x'
	if !bytes.Equal(got.ParamsRaw, compiled) {
		t.Fatal("borrowed buffer")
	}
	for _, field := range []string{"tenant_id", "actor_id", "auth_session_id", "pin", "runtime_sha", "capacity", "source_raw", "evidence"} {
		t.Run(field, func(t *testing.T) {
			var fields map[string]any
			json.Unmarshal(original, &fields)
			fields[field] = "injected"
			bad, _ := canonical.Marshal(fields)
			if _, e := DecodeRelightSubmissionRequest(bad); e == nil {
				t.Fatal("server field accepted")
			}
		})
	}
	for _, bad := range [][]byte{[]byte("null"), append(append([]byte(nil), original...), []byte("{}")...), []byte(strings.Repeat(" ", MaxRelightSubmissionBytes+1)), bytes.Replace(original, []byte(`"schema_version":1`), []byte(`"schema_version":1,"schema_version":1`), 1)} {
		if _, e := DecodeRelightSubmissionRequest(bad); e == nil {
			t.Fatal("invalid envelope")
		}
	}
	w.Ordinal = 5000
	bad, _ := canonical.Marshal(w)
	if _, e := DecodeRelightSubmissionRequest(bad); e == nil {
		t.Fatal("ordinal limit")
	}
}
