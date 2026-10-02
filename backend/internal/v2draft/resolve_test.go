package v2draft

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestResolveRecipeCombinesPinnedDependencies(t *testing.T) {
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(),
		Source:     v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)},
		CanvasHash: strings.Repeat("b", 64)}
	style := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}
	session := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("d", 64)}
	override := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("e", 64)}
	learn := uuid.New()
	add := ResolvedCorrection{Ref: session, Vector: CorrectionVector{EV: .1}}
	raw, err := canonical.Marshal(map[string]any{
		"schema_version": 3, "source": binding, "base_style": style,
		"session_corrections": session, "learning_version_id": learn,
		"overrides": []v2input.Ref{override}, "layer_stack": nil,
		"renderer_version": "v2.2",
		"parameter_delta":  CorrectionVector{RetouchDelta: .01},
	})
	if err != nil {
		t.Fatal(err)
	}
	inputs := ResolvedRecipeInputs{Binding: binding, BaseStyle: style,
		Style:   StyleBaseline{GainsRGB: [3]float64{1, 1, 1}, Saturation: 1},
		Session: &add, LearningID: learn, Learning: &CorrectionVector{LogR: .02},
		Overrides:  []ResolvedCorrection{{Ref: override, Vector: CorrectionVector{SaturationDelta: .03}}},
		ICCLockSHA: strings.Repeat("f", 64), ModelLockSHA: strings.Repeat("1", 64)}
	result, err := ResolveRecipe(raw, inputs)
	if err != nil {
		t.Fatal(err)
	}
	var frozen FrozenRecipe
	if err = json.Unmarshal(result, &frozen); err != nil || frozen.EffectiveDelta !=
		(CorrectionVector{EV: .1, LogR: .02, SaturationDelta: .03, RetouchDelta: .01}) {
		t.Fatal("incorrect resolved delta", frozen, err)
	}
	inputs.Learning = nil
	if _, err = ResolveRecipe(raw, inputs); err == nil {
		t.Fatal("missing learning prediction was accepted")
	}
	inputs.Learning = &CorrectionVector{LogR: .02}
	inputs.Overrides = nil
	if _, err = ResolveRecipe(raw, inputs); err == nil {
		t.Fatal("missing ordered override was accepted")
	}
	inputs.Overrides = []ResolvedCorrection{{Ref: override, Vector: CorrectionVector{SaturationDelta: .03}}}
	inputs.Overrides[0].Ref = session
	if _, err = ResolveRecipe(raw, inputs); err == nil {
		t.Fatal("wrong override artifact was accepted")
	}
	inputs.Overrides[0].Ref = override
	inputs.Style.GainsRGB[0] = 4
	if _, err = ResolveRecipe(raw, inputs); err == nil {
		t.Fatal("out-of-range effective StyleV2 was accepted")
	}
}
