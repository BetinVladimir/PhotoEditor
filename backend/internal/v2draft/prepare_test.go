package v2draft

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestPrepareDraftBindsPublishedLayerAndBase(t *testing.T) {
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(),
		Source:     v2input.Ref{ArtifactID: uuid.New(), SHA256: testDigest('a')},
		CanvasHash: testDigest('b')}
	style := v2input.Ref{ArtifactID: uuid.New(), SHA256: testDigest('c')}
	stale := v2input.Ref{ArtifactID: uuid.New(), SHA256: testDigest('d')}
	recipe, err := canonical.Marshal(map[string]any{
		"schema_version": 3, "source": binding, "base_style": style,
		"session_corrections": nil, "learning_version_id": nil,
		"overrides": []any{}, "layer_stack": stale, "renderer_version": "v2.2",
		"parameter_delta": map[string]any{"ev": 0, "log_r": 0, "log_g": 0,
			"log_b": 0, "saturation_delta": 0, "retouch_delta": 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	layers, err := canonical.Marshal(map[string]any{
		"schema_version": 1, "source": binding, "layers": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = PrepareDraft(recipe, []byte("null"), binding, nil, true); err == nil {
		t.Fatal("accepted a dangling client layer reference")
	}
	sum := sha256.Sum256(layers)
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	wrong := ref
	wrong.SHA256 = testDigest('e')
	if _, err = PrepareDraft(recipe, layers, binding, &wrong, true); err == nil {
		t.Fatal("accepted layer reference with a different body hash")
	}
	out, err := PrepareDraft(recipe, layers, binding, &ref, true)
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		LayerStack v2input.Ref `json:"layer_stack"`
	}
	if json.Unmarshal(out.Recipe, &saved) != nil || saved.LayerStack != ref ||
		len(out.Layers) == 0 || len(out.Base) == 0 {
		t.Fatalf("layer not bound to published ref: %+v", out)
	}
	if v2input.ValidateEditingRecipeV3(out.Base, binding) != nil {
		t.Fatal("base checkpoint invalid")
	}
}

func testDigest(ch byte) string {
	b := make([]byte, 64)
	for i := range b {
		b[i] = ch
	}
	return string(b)
}
