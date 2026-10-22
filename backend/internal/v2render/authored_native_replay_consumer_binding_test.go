package v2render

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestAuthoredNativeReplayConsumerBindingOwnsEffectiveTransport(t *testing.T) {
	entries := []v2input.Entry{{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, Contract: "AuthoredNativeReplayRequest"}}
	entryRaw, err := canonical.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	row := map[string]any{"base_snapshot": map[string]any{"immutable": true}, "base_input_digest": strings.Repeat("b", 64), "final_input_digest": strings.Repeat("c", 64), "entries_jcs": string(entryRaw), "request_evidence_roots": entries, "base_profile": map[string]any{"scratch": 1}, "final_profile": map[string]any{"scratch": 2}}
	raw, err := canonical.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	b, err := decodeAuthoredNativeReplayConsumerBinding(raw)
	if err != nil {
		t.Fatal(err)
	}
	original := b.CanonicalReadback()
	for i := range raw {
		raw[i] = 'x'
	}
	got := b.Entries()
	got[0].Contract = "foreign"
	p := b.ProfileJCS()
	p[0] = 'x'
	copyRaw := b.CanonicalReadback()
	copyRaw[0] = 'x'
	if !bytes.Equal(b.CanonicalReadback(), original) || b.Entries()[0].Contract != "AuthoredNativeReplayRequest" || b.ProfileJCS()[0] != '{' || b.InputDigest() != strings.Repeat("c", 64) {
		t.Fatal("effective transport ownership lost")
	}
	for _, field := range []string{"base_snapshot", "request_evidence_roots", "base_profile", "final_profile"} {
		t.Run("null_"+field, func(t *testing.T) {
			var altered map[string]any
			if json.Unmarshal(original, &altered) != nil {
				t.Fatal("fixture")
			}
			altered[field] = nil
			r, _ := canonical.Marshal(altered)
			if _, e := decodeAuthoredNativeReplayConsumerBinding(r); e == nil {
				t.Fatal("null accepted")
			}
		})
	}
	t.Run("unknown", func(t *testing.T) {
		row["foreign"] = true
		r, _ := canonical.Marshal(row)
		if _, e := decodeAuthoredNativeReplayConsumerBinding(r); e == nil {
			t.Fatal("unknown accepted")
		}
	})
}
