package v2learning

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

func TestCompileTrainingRecipesUsesPinnedV2Resolution(t *testing.T) {
	ref := func() v2input.Ref {
		return v2input.Ref{ArtifactID: uuid.New(),
			SHA256: strings.Repeat("a", 64)}
	}
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(),
		Source: ref(), CanvasHash: strings.Repeat("b", 64)}
	style := ref()
	body := func(ev float64, layer *v2input.Ref) []byte {
		encoded, err := canonical.Marshal(map[string]any{
			"schema_version": 3, "source": binding, "base_style": style,
			"session_corrections": nil, "learning_version_id": nil,
			"overrides": []any{}, "layer_stack": layer,
			"renderer_version": "v2.2",
			"parameter_delta": map[string]any{"ev": ev, "log_r": 0,
				"log_g": 0, "log_b": 0, "saturation_delta": 0,
				"retouch_delta": 0},
		})
		if err != nil {
			t.Fatal(err)
		}
		return encoded
	}
	documents := TrainingEditingDocuments{BaseBody: body(0, nil),
		ApprovedBody: body(.1, nil), Binding: binding, BaseStyle: style}
	inputs := v2draft.ResolvedRecipeInputs{Binding: binding,
		BaseStyle: style, Style: v2draft.StyleBaseline{
			GainsRGB: [3]float64{1, 1, 1}, Saturation: 1},
		ICCLockSHA:   strings.Repeat("c", 64),
		ModelLockSHA: strings.Repeat("d", 64)}
	evidence := TrainingRecipeEvidence{Inputs: inputs}
	got, err := CompileTrainingRecipes(documents, evidence, evidence)
	if err != nil || got.Base.EffectiveDelta.EV != 0 ||
		got.Approved.EffectiveDelta.EV != .1 ||
		!validSHA(got.BaseSHA) || !validSHA(got.ApprovedSHA) {
		t.Fatalf("frozen pair failed: %v", err)
	}
	forged := evidence
	forged.Inputs.LearningModelRef = ref()
	if _, err := CompileTrainingRecipes(documents, evidence, forged); err == nil {
		t.Fatal("unreferenced learning model accepted")
	}
	layerBody, err := canonical.Marshal(map[string]any{
		"schema_version": 1, "source": binding, "layers": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(layerBody)
	layer := v2input.Ref{ArtifactID: uuid.New(),
		SHA256: hex.EncodeToString(sum[:])}
	documents.ApprovedBody = body(.1, &layer)
	if _, err := CompileTrainingRecipes(documents, evidence,
		evidence); err == nil {
		t.Fatal("unverified layer stack accepted")
	}
	verifiedInactive := TrainingRecipeEvidence{Inputs: inputs,
		LayerStackRef: &layer, LayerStackBody: layerBody}
	if _, err := CompileTrainingRecipes(documents, evidence,
		verifiedInactive); err != nil {
		t.Fatal(err)
	}
	verifiedInactive.LayerStackBody = []byte(`{}`)
	if _, err := CompileTrainingRecipes(documents, evidence,
		verifiedInactive); err == nil {
		t.Fatal("unverified layer body accepted")
	}
}
