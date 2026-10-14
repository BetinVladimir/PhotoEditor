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

func TestFullRenderBoundsUseMergedAbsoluteModules(t *testing.T) {
	pin := func(raw []byte) v2input.Ref {
		sum := sha256.Sum256(raw)
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	}
	for _, tc := range []struct {
		name         string
		base, target float64
		valid        bool
	}{
		{"lowered override makes positive delta valid", 4, 1, true},
		{"raised override makes positive delta invalid", 1, 4, false},
		{"ordinary absolute module", 1, 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			global := func(gain float64) any {
				return map[string]any{"gains_rgb": []float64{gain, gain, gain}, "saturation": 1}
			}
			style, _ := canonical.Marshal(map[string]any{"schema_version": "2.0.0", "renderer_version": "photo_renderer_v2", "global": global(tc.base), "beauty": map[string]any{"enabled": false}, "extensions": map[string]any{}})
			encodedSHA := strings.Repeat("a", 64)
			override, _ := canonical.Marshal(map[string]any{"schema_version": 1, "input_sha256": encodedSHA, "modules": map[string]any{"global": global(tc.target)}, "revision": 1})
			ref := pin(override)
			inputs := ResolvedRecipeInputs{Binding: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: pin([]byte("source")), CanvasHash: strings.Repeat("b", 64)}, BaseStyle: pin(style), Style: StyleBaseline{GainsRGB: [3]float64{tc.base, tc.base, tc.base}, Saturation: 1}, Overrides: []ResolvedCorrection{{Ref: ref}}, ICCLockSHA: strings.Repeat("c", 64), ModelLockSHA: strings.Repeat("d", 64)}
			authored, _ := canonical.Marshal(map[string]any{"schema_version": 3, "source": inputs.Binding, "base_style": inputs.BaseStyle, "session_corrections": nil, "learning_version_id": nil, "overrides": []v2input.Ref{ref}, "layer_stack": nil, "renderer_version": "v2.2", "parameter_delta": CorrectionVector{EV: .1}})
			frozen, err := BuildFrozenRenderRecipeFromDocuments(authored, inputs, nil, style, [][]byte{override}, encodedSHA)
			if (err == nil) != tc.valid {
				t.Fatal("incorrect merged bounds", err)
			}
			_, selectErr := SelectRenderBackgroundAsset(authored, inputs, style, [][]byte{override}, encodedSHA)
			if (selectErr == nil) != tc.valid {
				t.Fatal("background path uses different bounds", selectErr)
			}
			if tc.valid {
				decoded, err := DecodeFrozenRenderRecipe(frozen)
				if err != nil || decoded.Scalars.EffectiveDelta.EV != .1 || decoded.Scalars.BaseStyle != inputs.BaseStyle {
					t.Fatal("frozen provenance changed", err)
				}
				_, err = (Publisher{}).CompileAndPublishSelectedFrozenRenderRecipe(context.Background(), auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), authored, inputs, nil, style, [][]byte{override}, encodedSHA, nil)
				if err == nil || !strings.Contains(err.Error(), "DEPENDENCY_UNAVAILABLE") {
					t.Fatal("valid merged recipe did not reach publication boundary", err)
				}
			} else {
				_, err = (Publisher{}).CompileAndPublishSelectedFrozenRenderRecipe(context.Background(), auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), authored, inputs, nil, style, [][]byte{override}, encodedSHA, nil)
				if err == nil || !strings.Contains(err.Error(), "VALIDATION_FAILED") {
					t.Fatal("invalid merged recipe reached publication boundary", err)
				}
			}

			_, fencedErr := (Publisher{}).CompileAndPublishSelectedFrozenRenderDraftRecipe(context.Background(), auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), uuid.New(), 1, authored, inputs, nil, style, [][]byte{override}, encodedSHA, nil)
			expectedCode := "VALIDATION_FAILED"
			if tc.valid {
				expectedCode = "DEPENDENCY_UNAVAILABLE"
			}
			if fencedErr == nil || !strings.Contains(fencedErr.Error(), expectedCode) {
				t.Fatal("fenced publisher uses different merged bounds", fencedErr)
			}
			if inputs.Style.GainsRGB[0] != tc.base {
				t.Fatal("base scalar authority mutated")
			}
		})
	}
}

func TestFullRenderScalarModuleShapeAndBeautyBounds(t *testing.T) {
	pin := func(raw []byte) v2input.Ref {
		sum := sha256.Sum256(raw)
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	}
	for _, tc := range []struct {
		name, modules string
		delta         CorrectionVector
		valid         bool
	}{
		{"null saturation", `{"global":{"gains_rgb":[1,1,1],"saturation":null}}`, CorrectionVector{}, false},
		{"missing saturation", `{"global":{"gains_rgb":[1,1,1]}}`, CorrectionVector{}, false},
		{"null gain", `{"global":{"gains_rgb":[1,null,1],"saturation":1}}`, CorrectionVector{}, false},
		{"short gains", `{"global":{"gains_rgb":[1,1],"saturation":1}}`, CorrectionVector{}, false},
		{"invalid gain", `{"global":{"gains_rgb":[1,5,1],"saturation":1}}`, CorrectionVector{}, false},
		{"saturation final overflow", `{"global":{"gains_rgb":[1,1,1],"saturation":2}}`, CorrectionVector{SaturationDelta: .1}, false},
		{"null beauty flag", `{"beauty":{"enabled":null}}`, CorrectionVector{}, false},
		{"missing smoothing", `{"beauty":{"enabled":true}}`, CorrectionVector{}, false},
		{"null smoothing", `{"beauty":{"enabled":true,"smoothing":null}}`, CorrectionVector{}, false},
		{"beauty final overflow", `{"beauty":{"enabled":true,"smoothing":0.9}}`, CorrectionVector{RetouchDelta: .2}, false},
		{"beauty final valid", `{"beauty":{"enabled":true,"smoothing":0.4}}`, CorrectionVector{RetouchDelta: .1}, true},
		{"disabled beauty reset", `{"beauty":{"enabled":false}}`, CorrectionVector{RetouchDelta: .1}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			style, _ := canonical.Marshal(map[string]any{"schema_version": "2.0.0", "renderer_version": "photo_renderer_v2", "global": map[string]any{"gains_rgb": []float64{1, 1, 1}, "saturation": 1}, "beauty": map[string]any{"enabled": false}, "extensions": map[string]any{}})
			override, _ := canonical.Bytes([]byte(`{"schema_version":1,"input_sha256":"` + strings.Repeat("a", 64) + `","modules":` + tc.modules + `,"revision":1}`))
			ref := pin(override)
			inputs := ResolvedRecipeInputs{Binding: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: pin([]byte("source")), CanvasHash: strings.Repeat("b", 64)}, BaseStyle: pin(style), Style: StyleBaseline{GainsRGB: [3]float64{1, 1, 1}, Saturation: 1}, Overrides: []ResolvedCorrection{{Ref: ref}}, ICCLockSHA: strings.Repeat("c", 64), ModelLockSHA: strings.Repeat("d", 64)}
			authored, _ := canonical.Marshal(map[string]any{"schema_version": 3, "source": inputs.Binding, "base_style": inputs.BaseStyle, "session_corrections": nil, "learning_version_id": nil, "overrides": []v2input.Ref{ref}, "layer_stack": nil, "renderer_version": "v2.2", "parameter_delta": tc.delta})
			_, err := BuildFrozenRenderRecipeFromDocuments(authored, inputs, nil, style, [][]byte{override}, strings.Repeat("a", 64))
			if (err == nil) != tc.valid {
				t.Fatal("wrong scalar module validation", err)
			}
			if tc.valid {
				inputs.Overrides[0].Vector.EV = .1
				if _, err := BuildFrozenRenderRecipeFromDocuments(authored, inputs, nil, style, [][]byte{override}, strings.Repeat("a", 64)); err == nil {
					t.Fatal("module scalar double application accepted")
				}
			}
		})
	}
}
