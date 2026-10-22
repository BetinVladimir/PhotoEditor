package v2draft

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"math"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func editorEffectsFixture(t *testing.T, modules map[string]any, delta CorrectionVector) ([]byte, ResolvedRecipeInputs, []byte, [][]byte, string) {
	t.Helper()
	style, e := os.ReadFile("../run/identity_style.json")
	if e != nil {
		t.Fatal(e)
	}
	style, e = canonical.Bytes(style)
	if e != nil {
		t.Fatal(e)
	}
	pin := func(raw []byte) v2input.Ref {
		sum := sha256.Sum256(raw)
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	}
	source := strings.Repeat("a", 64)
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}, CanvasHash: strings.Repeat("c", 64)}
	baseline, e := DecodeStyleBaseline(style)
	if e != nil {
		t.Fatal(e)
	}
	inputs := ResolvedRecipeInputs{Binding: binding, BaseStyle: pin(style), Style: baseline, ICCLockSHA: strings.Repeat("d", 64), ModelLockSHA: strings.Repeat("e", 64)}
	bodies := [][]byte{}
	refs := []v2input.Ref{}
	if modules != nil {
		override, e := canonical.Marshal(map[string]any{"schema_version": 1, "input_sha256": source, "revision": 1, "modules": modules})
		if e != nil {
			t.Fatal(e)
		}
		ref := pin(override)
		bodies = append(bodies, override)
		refs = append(refs, ref)
		inputs.Overrides = append(inputs.Overrides, ResolvedCorrection{Ref: ref})
	}
	authored, e := canonical.Marshal(map[string]any{"schema_version": 3, "source": binding, "base_style": inputs.BaseStyle, "session_corrections": nil, "learning_version_id": nil, "overrides": refs, "layer_stack": nil, "renderer_version": "v2.2", "parameter_delta": delta})
	if e != nil {
		t.Fatal(e)
	}
	return authored, inputs, style, bodies, source
}
func TestEditorEffectsMaterializesScalarsOnceAndRetainsFullStages(t *testing.T) {
	modules := map[string]any{"strength": .4, "defects": map[string]any{"enabled": true, "algorithm": "defectrepair_telea_v1", "policy": "review", "strength": .5, "diameter_face_fraction": []float64{.002, .012}, "preserve_permanent_marks": true}}
	authored, inputs, style, bodies, source := editorEffectsFixture(t, modules, CorrectionVector{EV: .1, RetouchDelta: .1})
	result, e := CompileEditorEffects(authored, inputs, style, bodies, source)
	if e != nil {
		t.Fatal(e)
	}
	var effective struct {
		Strength float64 `json:"strength"`
		Global   struct {
			Gains []float64 `json:"gains_rgb"`
		} `json:"global"`
		Beauty struct {
			Enabled   bool    `json:"enabled"`
			Smoothing float64 `json:"smoothing"`
		} `json:"beauty"`
	}
	if json.Unmarshal(result.Style, &effective) != nil || effective.Strength != .4 || !effective.Beauty.Enabled || effective.Beauty.Smoothing != .1 || math.Abs(effective.Global.Gains[0]-math.Pow(2, .1)) > 1e-12 {
		t.Fatal("incorrect scalar materialization")
	}
	phases := map[string]int{}
	for i, stage := range result.Stages {
		phases[stage.Phase] = i
	}
	for _, name := range []string{"face_detect", "beauty", "defects_propose", "defects_review", "quality"} {
		if _, ok := phases[name]; !ok {
			t.Fatal("requested analysis/effect/barrier omitted", name)
		}
	}
	if phases["defects_review"] >= phases["quality"] {
		t.Fatal("quality preceded human barrier")
	}
	again, e := CompileEditorEffects(authored, inputs, style, bodies, source)
	if e != nil || string(again.Style) != string(result.Style) || string(again.Override) != string(result.Override) {
		t.Fatal("compiler is not reproducible")
	}
}
func TestEditorEffectsRejectsChangedOrderedPinsAndForeignSource(t *testing.T) {
	authored, inputs, style, bodies, source := editorEffectsFixture(t, map[string]any{"strength": .5}, CorrectionVector{})
	if _, e := CompileEditorEffects(authored, inputs, style, bodies, strings.Repeat("f", 64)); e == nil {
		t.Fatal("foreign encoded source accepted")
	}
	changed := append([]byte(nil), style...)
	changed = append(changed, ' ')
	if _, e := CompileEditorEffects(authored, inputs, changed, bodies, source); e == nil {
		t.Fatal("noncanonical/changed style accepted")
	}
	if _, e := CompileEditorEffects(authored, inputs, style, nil, source); e == nil {
		t.Fatal("missing ordered override admitted")
	}
}

// An empty V2 extensions replacement clears every accepted-base extension.
// Apply's partial overlay must receive explicit nulls, not an empty object.
func TestEditorEffectsEmitsExplicitExtensionClears(t *testing.T) {
	authored, inputs, style, bodies, source := editorEffectsFixture(t, map[string]any{"extensions": map[string]any{}}, CorrectionVector{})
	result, err := CompileEditorEffects(authored, inputs, style, bodies, source)
	if err != nil {
		t.Fatal(err)
	}
	var override struct {
		Modules struct {
			Extensions map[string]json.RawMessage `json:"extensions"`
		} `json:"modules"`
	}
	if err = json.Unmarshal(result.Override, &override); err != nil {
		t.Fatal(err)
	}
	for _, slot := range []string{"culling", "series", "composition", "portrait_detail", "masking", "restoration", "background", "inpainting", "generation"} {
		if string(override.Modules.Extensions[slot]) != "null" {
			t.Fatalf("extension %s would survive Apply overlay", slot)
		}
	}
	if string(override.Modules.Extensions["region_adjustments"]) != "[]" {
		t.Fatal("region adjustments were not explicitly cleared")
	}
}

func TestEditorEffectCompilerInputStages(t *testing.T) {
	authored, inputs, style, bodies, source := editorEffectsFixture(t, nil, CorrectionVector{})
	baseline, _, err := resolveRenderModuleDocuments(authored, inputs, style, bodies, source)
	if err != nil {
		t.Fatal("module documents", err)
	}
	inputs.Style = baseline
	if _, err = ResolveRecipe(authored, inputs); err != nil {
		t.Fatal("recipe", err)
	}
}
