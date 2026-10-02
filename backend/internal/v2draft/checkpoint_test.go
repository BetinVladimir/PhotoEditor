package v2draft

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestBaseCheckpointKeepsSourceAndStyleWithoutUserEdits(t *testing.T) {
	source := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	style := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}
	protection := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("f", 64)}
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(),
		Source: source, CanvasHash: strings.Repeat("c", 64)}
	user, err := canonical.Marshal(map[string]any{
		"schema_version": 3, "source": binding, "base_style": style,
		"session_corrections": nil, "learning_version_id": nil,
		"overrides":   []v2input.Ref{{ArtifactID: uuid.New(), SHA256: strings.Repeat("d", 64)}},
		"layer_stack": nil, "renderer_version": "v2.2",
		"protected_mask": protection,
		"parameter_delta": map[string]any{"ev": 0.25, "log_r": 0.1,
			"log_g": 0, "log_b": 0, "saturation_delta": 0.05,
			"retouch_delta": 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	base, err := BaseCheckpoint(user, binding)
	if err != nil {
		t.Fatal(err)
	}
	if err := v2input.ValidateEditingRecipeV3(base, binding); err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Source         v2input.SessionVersionBinding `json:"source"`
		BaseStyle      v2input.Ref                   `json:"base_style"`
		ProtectedMask  *v2input.Ref                  `json:"protected_mask"`
		Overrides      []v2input.Ref                 `json:"overrides"`
		ParameterDelta struct {
			EV   float64 `json:"ev"`
			LogR float64 `json:"log_r"`
		} `json:"parameter_delta"`
	}
	if err := json.Unmarshal(base, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Source != binding || decoded.BaseStyle != style ||
		decoded.ProtectedMask == nil || *decoded.ProtectedMask != protection ||
		len(decoded.Overrides) != 0 || decoded.ParameterDelta.EV != 0 ||
		decoded.ParameterDelta.LogR != 0 {
		t.Fatalf("base checkpoint retained user edits: %s", base)
	}
	binding.CanvasHash = strings.Repeat("e", 64)
	if _, err := BaseCheckpoint(user, binding); err == nil {
		t.Fatal("accepted recipe for a different source binding")
	}
}
