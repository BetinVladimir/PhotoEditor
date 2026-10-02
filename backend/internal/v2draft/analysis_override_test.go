package v2draft

import (
	"encoding/json"
	"math"
	"os"
	"reflect"
	"testing"

	"photoeditor/backend/internal/canonical"
)

func TestBuildAnalysisOverridePreservesStyleAndBindsSource(t *testing.T) {
	style, err := os.ReadFile("../run/identity_style.json")
	if err != nil {
		t.Fatal(err)
	}
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	correction := CorrectionVector{EV: .25, LogR: .05,
		SaturationDelta: .1, RetouchDelta: .1}
	body, err := BuildAnalysisOverride(style, sha, correction)
	if err != nil {
		t.Fatal(err)
	}
	var original struct {
		Global map[string]json.RawMessage `json:"global"`
	}
	var result struct {
		InputSHA256 string `json:"input_sha256"`
		Modules     struct {
			Global struct {
				GainsRGB    []float64       `json:"gains_rgb"`
				Saturation  float64         `json:"saturation"`
				MasterCurve json.RawMessage `json:"master_curve"`
				RGBCurves   json.RawMessage `json:"rgb_curves"`
				ResidualLUT json.RawMessage `json:"residual_lut"`
			} `json:"global"`
			Beauty struct {
				Enabled      bool    `json:"enabled"`
				Smoothing    float64 `json:"smoothing"`
				ToneEvenness float64 `json:"tone_evenness"`
				TextureKeep  float64 `json:"texture_keep"`
			} `json:"beauty"`
		} `json:"modules"`
	}
	if json.Unmarshal(style, &original) != nil || json.Unmarshal(body, &result) != nil {
		t.Fatal("invalid compiled override")
	}
	if result.InputSHA256 != sha || !result.Modules.Beauty.Enabled ||
		result.Modules.Beauty.Smoothing != .1 || result.Modules.Beauty.ToneEvenness != 0 ||
		result.Modules.Beauty.TextureKeep != 1 ||
		math.Abs(result.Modules.Global.GainsRGB[0]-math.Pow(2, .25)*math.Exp(.05)) > 1e-12 ||
		result.Modules.Global.Saturation != 1.1 {
		t.Fatalf("incorrect scalar correction: %+v", result)
	}
	var compiled map[string]json.RawMessage
	var modules map[string]json.RawMessage
	var global map[string]json.RawMessage
	if json.Unmarshal(body, &compiled) != nil ||
		json.Unmarshal(compiled["modules"], &modules) != nil ||
		json.Unmarshal(modules["global"], &global) != nil {
		t.Fatal("invalid global override")
	}
	for _, key := range []string{"master_curve", "rgb_curves", "residual_lut"} {
		var before, after any
		if json.Unmarshal(original.Global[key], &before) != nil ||
			json.Unmarshal(global[key], &after) != nil || !reflect.DeepEqual(before, after) {
			t.Fatalf("baseline %s changed", key)
		}
	}
	var highStyle map[string]json.RawMessage
	if json.Unmarshal(style, &highStyle) != nil {
		t.Fatal("invalid style fixture")
	}
	var highGlobal map[string]json.RawMessage
	if json.Unmarshal(highStyle["global"], &highGlobal) != nil {
		t.Fatal("invalid global fixture")
	}
	highGlobal["gains_rgb"] = json.RawMessage(`[4,1,1]`)
	highStyle["global"], _ = json.Marshal(highGlobal)
	highRaw, err := canonical.Marshal(highStyle)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := BuildAnalysisOverride(highRaw, sha, CorrectionVector{EV: 1}); err == nil {
		t.Fatal("out-of-bounds effective style accepted")
	}
}
