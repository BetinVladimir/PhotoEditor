package v2learning

import (
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestTrainingDocumentPairRequiresEditingRecipeSourceAndStyle(t *testing.T) {
	asset := uuid.New()
	ref := func() v2input.Ref {
		return v2input.Ref{ArtifactID: uuid.New(),
			SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	}
	binding := v2input.SessionVersionBinding{AssetVersionID: asset,
		Source:     ref(),
		CanvasHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	style := ref()
	recipe := func(source v2input.SessionVersionBinding,
		base v2input.Ref) []byte {
		body, err := canonical.Marshal(map[string]any{
			"schema_version": 3, "source": source, "base_style": base,
			"session_corrections": nil, "learning_version_id": nil,
			"overrides": []any{}, "layer_stack": nil,
			"renderer_version": "v2.2",
			"parameter_delta": map[string]any{"ev": 0, "log_r": 0,
				"log_g": 0, "log_b": 0, "saturation_delta": 0,
				"retouch_delta": 0},
		})
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	base := recipe(binding, style)
	if source, foundStyle, err := ValidateTrainingEditingPair(base, base,
		asset); err != nil || source != binding || foundStyle != style {
		t.Fatalf("valid editing pair rejected: %v", err)
	}
	wrong := binding
	wrong.Source = ref()
	if _, _, err := ValidateTrainingEditingPair(base, recipe(wrong, style),
		asset); err == nil {
		t.Fatal("mixed source accepted")
	}
	if _, _, err := ValidateTrainingEditingPair(base, recipe(binding, ref()),
		asset); err == nil {
		t.Fatal("mixed base style accepted")
	}
	if _, _, err := ValidateTrainingEditingPair(base, base,
		uuid.New()); err == nil {
		t.Fatal("wrong asset version accepted")
	}
}
