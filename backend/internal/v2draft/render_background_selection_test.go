package v2draft

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestSelectRenderBackgroundOrderedWholeModules(t *testing.T) {
	sourceSHA := strings.Repeat("a", 64)
	background := func(id string) any {
		return map[string]any{"mode": "asset", "asset_id": id, "blur_strength": 0, "protect_subject": true}
	}
	style, _ := canonical.Marshal(map[string]any{"schema_version": "2.0.0", "renderer_version": "photo_renderer_v2", "global": map[string]any{"gains_rgb": []float64{1, 1, 1}, "saturation": 1}, "beauty": map[string]any{"enabled": false, "smoothing": 0}, "extensions": map[string]any{"background": background("base")}})
	ref := func(raw []byte) v2input.Ref {
		sum := sha256.Sum256(raw)
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	}
	inputs := ResolvedRecipeInputs{Binding: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: sourceSHA}, CanvasHash: strings.Repeat("b", 64)}, BaseStyle: ref(style), Style: StyleBaseline{GainsRGB: [3]float64{1, 1, 1}, Saturation: 1}, ICCLockSHA: strings.Repeat("c", 64), ModelLockSHA: strings.Repeat("d", 64)}
	cases := []struct {
		name     string
		modules  []any
		expected string
	}{
		{"base", nil, "base"},
		{"unrelated", []any{map[string]any{"strength": .5}}, "base"},
		{"replace", []any{map[string]any{"extensions": map[string]any{"background": background("first")}}, map[string]any{"extensions": map[string]any{"background": background("last")}}}, "last"},
		{"clear absent", []any{map[string]any{"extensions": map[string]any{}}}, ""},
		{"clear null", []any{map[string]any{"extensions": map[string]any{"background": nil}}}, ""},
		{"keep", []any{map[string]any{"extensions": map[string]any{"background": map[string]any{"mode": "keep", "asset_id": "unused", "blur_strength": 0, "protect_subject": true}}}}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			current := inputs
			current.Overrides = nil
			var bodies [][]byte
			var refs []v2input.Ref = make([]v2input.Ref, 0)
			for _, modules := range tc.modules {
				raw, _ := canonical.Marshal(map[string]any{"schema_version": 1, "input_sha256": sourceSHA, "modules": modules, "revision": 1})
				pin := ref(raw)
				refs = append(refs, pin)
				bodies = append(bodies, raw)
				current.Overrides = append(current.Overrides, ResolvedCorrection{Ref: pin})
			}
			authored, _ := canonical.Marshal(map[string]any{"schema_version": 3, "source": current.Binding, "base_style": current.BaseStyle, "session_corrections": nil, "learning_version_id": nil, "overrides": refs, "layer_stack": nil, "renderer_version": "v2.2", "parameter_delta": CorrectionVector{}})
			got, err := SelectRenderBackgroundAsset(authored, current, style, bodies, sourceSHA)
			if err != nil || got != tc.expected {
				t.Fatalf("got %q %v", got, err)
			}
			if _, err := SelectRenderBackgroundAsset(authored, current, append(append([]byte(nil), style...), ' '), bodies, sourceSHA); err == nil {
				t.Fatal("changed style accepted")
			}
			if len(bodies) > 0 {
				changed := append([][]byte(nil), bodies...)
				changed[0] = []byte(`{}`)
				if _, err := SelectRenderBackgroundAsset(authored, current, style, changed, sourceSHA); err == nil {
					t.Fatal("changed override accepted")
				}
				if _, err := SelectRenderBackgroundAsset(authored, current, style, bodies, strings.Repeat("f", 64)); err == nil {
					t.Fatal("foreign encoded source accepted")
				}
			}
			if _, err := (Publisher{}).CompileAndPublishSelectedFrozenRenderRecipe(context.Background(), auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), authored, current, nil, style, bodies, sourceSHA, nil); err == nil {
				t.Fatal("unconfigured publisher accepted publication")
			}
			if tc.expected == "" {
				assume := "srgb_v1"
				_, err := (Publisher{}).CompileAndPublishSelectedFrozenRenderRecipe(context.Background(), auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), authored, current, nil, style, bodies, sourceSHA, &assume)
				if err == nil || !strings.Contains(err.Error(), "VALIDATION_FAILED") {
					t.Fatal("irrelevant ICC assumption accepted", err)
				}
			}
		})
	}
}

func TestRenderBackgroundRejectsMalformedModule(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `{"background":{}}`, `{"background":{"mode":"unknown","blur_strength":0,"protect_subject":true}}`, `{"background":{"mode":"asset","asset_id":"","blur_strength":0,"protect_subject":true}}`, `{"background":{"mode":"solid","solid_rgb":[2,0,0],"blur_strength":0,"protect_subject":true}}`, `{"background":{"mode":"keep","blur_strength":0,"protect_subject":false}}`} {
		if _, err := backgroundPortableID([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
