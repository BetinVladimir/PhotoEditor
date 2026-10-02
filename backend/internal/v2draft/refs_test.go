package v2draft

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestDirectRefsOrderAndPaths(t *testing.T) {
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: hash} }
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(), CanvasHash: hash}
	style, correction, override, layers, protection := ref(), ref(), ref(), ref(), ref()
	input := map[string]any{
		"schema_version": 3, "source": binding, "base_style": style,
		"session_corrections": correction, "learning_version_id": nil,
		"overrides": []v2input.Ref{override}, "layer_stack": layers,
		"protected_mask":   protection,
		"renderer_version": "v2.2", "parameter_delta": map[string]any{
			"ev": 0, "log_r": 0, "log_g": 0, "log_b": 0,
			"saturation_delta": 0, "retouch_delta": 0,
		},
	}
	raw, err := canonical.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := DirectRefs(raw, binding)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 6 || refs[0].ArtifactID != binding.Source.ArtifactID.String() ||
		refs[1].ArtifactID != style.ArtifactID.String() ||
		refs[2].ArtifactID != correction.ArtifactID.String() ||
		refs[3].ArtifactID != override.ArtifactID.String() ||
		refs[4].ArtifactID != layers.ArtifactID.String() ||
		refs[5].ArtifactID != protection.ArtifactID.String() {
		t.Fatalf("unexpected ref order: %+v", refs)
	}
	for i, item := range refs {
		if item.Ordinal != i || len(item.Purpose) != 64 {
			t.Fatalf("invalid ref %d: %+v", i, item)
		}
	}
	var decoded []map[string]any
	encoded, err := json.Marshal(refs)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil || len(decoded[0]) != 4 {
		t.Fatalf("invalid SQL wire shape: %v %v", decoded, err)
	}
}
