package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"sort"
	"testing"
)

func authoredFinalReplayTransportFixture(t *testing.T) (AuthoredFinalTaskPlan, []v2input.Entry, map[string]any) {
	t.Helper()
	e, ref, p := authoredFinalTaskCompilerFixture(t)
	raw, _ := canonical.Marshal(p)
	base, err := compileAuthoredFinalTask(e, ref, raw)
	if err != nil {
		t.Fatal(err)
	}
	roots := []v2input.Entry{{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: ref.SHA256}, Contract: "AuthoredNativeReplayRequest"}, {Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: ref.SHA256}, Contract: "AuthoredNativeReplayEvidence"}}
	entries := append(append([]v2input.Entry(nil), base.Entries...), roots...)
	sort.Slice(entries, func(i, j int) bool {
		return bytes.Compare(entries[i].Ref.ArtifactID[:], entries[j].Ref.ArtifactID[:]) < 0
	})
	entryRaw, _ := canonical.Marshal(entries)
	digest, _ := canonical.Hash(struct {
		Task      uuid.UUID       `json:"task"`
		Operation string          `json:"operation"`
		Version   string          `json:"version"`
		Entries   []v2input.Entry `json:"entries"`
	}{base.Task.ID, "render.compose", "2", entries})
	var profile map[string]any
	json.Unmarshal(raw, &profile)
	resources := profile["resource_estimate"].(map[string]any)
	resources["scratch_bytes"] = resources["scratch_bytes"].(float64) + 16777216
	profile["input_index_scratch_bytes"] = 16777216
	profile["contracts_sha"] = "338b60d6fd3e232eb78ceb71d559a8eb4a9e82170d1088d4484a09907d19984b"
	return base, entries, map[string]any{"base_snapshot": map[string]any{"params": base.ParamsRef}, "base_input_digest": base.Task.InputDigest, "final_input_digest": digest, "entries_jcs": string(entryRaw), "request_evidence_roots": roots, "base_profile": json.RawMessage(raw), "final_profile": profile}
}

func TestAuthoredFinalBoundReplayTransportPreservesBaseAndActualCompute(t *testing.T) {
	base, entries, binding := authoredFinalReplayTransportFixture(t)
	before, _ := canonical.Marshal(base)
	raw, _ := canonical.Marshal(binding)
	selected, err := compileAuthoredFinalBoundTransport(base, entries, raw)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := canonical.Marshal(base)
	if !bytes.Equal(before, after) || selected.validateFrozen() != nil || selected.Task.ID != base.Task.ID || selected.ParamsRef != base.ParamsRef || selected.Task.InputDigest == base.Task.InputDigest {
		t.Fatal("base mutation or ineffective transport")
	}
	selected.Entries[0].Contract = "corrupt"
	if base.validateFrozen() != nil || selected.validateFrozen() == nil {
		t.Fatal("transport ownership")
	}
}
func TestAuthoredFinalBoundReplayTransportRejectsPolicyAndRootSubstitution(t *testing.T) {
	for _, name := range []string{"digest", "base", "backend", "review", "ram", "scratch", "schema", "roots", "entries", "unknown"} {
		t.Run(name, func(t *testing.T) {
			base, entries, b := authoredFinalReplayTransportFixture(t)
			p := b["final_profile"].(map[string]any)
			switch name {
			case "digest":
				b["final_input_digest"] = base.Task.InputDigest
			case "base":
				b["base_input_digest"] = ""
			case "backend":
				p["backend"] = "mps"
			case "review":
				p["review_required"] = false
			case "ram":
				p["resource_estimate"].(map[string]any)["ram_bytes"] = 999
			case "scratch":
				p["resource_estimate"].(map[string]any)["scratch_bytes"] = 1
			case "schema":
				p["contracts_sha"] = "old"
			case "roots":
				b["request_evidence_roots"] = []v2input.Entry{}
			case "entries":
				b["entries_jcs"] = "[]"
			case "unknown":
				b["override"] = true
			}
			raw, _ := canonical.Marshal(b)
			if _, err := compileAuthoredFinalBoundTransport(base, entries, raw); err == nil {
				t.Fatal("substitution accepted")
			}
		})
	}
}
func TestAuthoredFinalReplayBindingCancellationPrecedesDatabase(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (SQLAuthoredFinalAdmission{}).BindReplayTransport(ctx, uuid.New(), AuthoredFinalTaskPlan{}, nil); err != context.Canceled {
		t.Fatal(err)
	}
}
