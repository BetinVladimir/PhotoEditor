package v2learning

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestTrainingLayerActivityValidatesSourceBinding(t *testing.T) {
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(),
		Source: v2input.Ref{ArtifactID: uuid.New(),
			SHA256: strings.Repeat("a", 64)},
		CanvasHash: strings.Repeat("b", 64)}
	body, err := canonical.Marshal(map[string]any{
		"schema_version": 1, "source": binding, "layers": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	active, err := TrainingLayerActivity(body, binding)
	if err != nil || active {
		t.Fatalf("empty stack invalid: %v", err)
	}
	wrong := binding
	wrong.AssetVersionID = uuid.New()
	if _, err := TrainingLayerActivity(body, wrong); err == nil {
		t.Fatal("cross-source stack accepted")
	}
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}
	layer := map[string]any{"id": uuid.New(), "enabled": true,
		"opacity": .5, "blend": "normal", "operation": "adjust.exposure",
		"operation_version": "1", "params": ref,
		"mask": map[string]any{"op": "ref", "mask": ref,
			"canvas_hash": binding.CanvasHash}}
	stack := map[string]any{"schema_version": 1, "source": binding,
		"layers": []any{layer}}
	body, err = canonical.Marshal(stack)
	if err != nil {
		t.Fatal(err)
	}
	active, err = TrainingLayerActivity(body, binding)
	if err != nil || !active {
		t.Fatalf("active layer missed: %v", err)
	}
	layer["opacity"] = 0
	body, err = canonical.Marshal(stack)
	if err != nil {
		t.Fatal(err)
	}
	active, err = TrainingLayerActivity(body, binding)
	if err != nil || active {
		t.Fatalf("zero-opacity layer marked active: %v", err)
	}
}
