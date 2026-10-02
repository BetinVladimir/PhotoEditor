package v2render

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
)

func TestDecodeRenderBatchManifestPinsScopedCompactEvidence(t *testing.T) {
	batch, session := uuid.New(), uuid.New()
	value := RenderBatchManifest{PolicySHA: strings.Repeat("a", 64), SchemaVersion: 1, ProjectID: uuid.New(), SnapshotID: uuid.New(), SessionID: session, Parts: []RenderBatchPartEvidence{
		{RunID: uuid.NewSHA1(batch, []byte("render.batch:0")), FirstOrdinal: 0, FrameCount: 2500, ConfigSHA: strings.Repeat("a", 64), OutputBytes: 1000},
		{RunID: uuid.NewSHA1(batch, []byte("render.batch:1")), FirstOrdinal: 2500, FrameCount: 2500, ConfigSHA: strings.Repeat("b", 64), OutputBytes: 2000},
	}}
	wire, _ := canonical.Marshal(value)
	sha, _ := canonical.Hash(json.RawMessage(wire))
	spaced, _ := json.MarshalIndent(value, "", "  ")
	for _, raw := range [][]byte{wire, spaced} {
		got, err := DecodeRenderBatchManifest(batch, session, raw, sha)
		if err != nil || len(got.Parts) != 2 || got.Parts[1].FrameCount != 2500 {
			t.Fatal("persisted batch decode failed", err)
		}
	}
	if _, err := DecodeRenderBatchManifest(batch, uuid.New(), wire, sha); err == nil {
		t.Fatal("foreign session accepted")
	}
	if _, err := DecodeRenderBatchManifest(batch, session, wire, strings.Repeat("c", 64)); err == nil {
		t.Fatal("wrong manifest digest accepted")
	}
	for _, mutation := range []func(map[string]any){
		func(v map[string]any) { v["extra"] = true },
		func(v map[string]any) { v["schema_version"] = 2 },
		func(v map[string]any) { v["parts"].([]any)[1].(map[string]any)["first_ordinal"] = 2501 },
		func(v map[string]any) { v["parts"].([]any)[1].(map[string]any)["frame_count"] = 2501 },
		func(v map[string]any) { v["parts"].([]any)[1].(map[string]any)["run_id"] = uuid.New().String() },
		func(v map[string]any) { v["parts"].([]any)[1].(map[string]any)["config_sha"] = "invalid" },
	} {
		var altered map[string]any
		if json.Unmarshal(wire, &altered) != nil {
			t.Fatal("fixture decode failed")
		}
		mutation(altered)
		changed, _ := canonical.Marshal(altered)
		pin, _ := canonical.Hash(json.RawMessage(changed))
		if _, err := DecodeRenderBatchManifest(batch, session, changed, pin); err == nil {
			t.Fatal("rehashed invalid batch manifest accepted")
		}
	}
}
