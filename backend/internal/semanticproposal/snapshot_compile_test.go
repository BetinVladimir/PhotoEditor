package semanticproposal

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestTargetSnapshotRequiresExactRecipeSourceAndDigest(t *testing.T) {
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref, CanvasHash: strings.Repeat("b", 64)}
	inventory := CatalogInventory{Scope: CatalogScope{ProjectID: uuid.New(), ItemCount: 1}, Targets: []v2input.SessionVersionBinding{binding}}
	body, err := canonical.Marshal(map[string]any{"schema_version": 3, "source": binding, "base_style": ref, "session_corrections": nil, "learning_version_id": nil, "overrides": []any{}, "layer_stack": nil, "renderer_version": "v2.2", "parameter_delta": map[string]any{"ev": 0, "log_r": 0, "log_g": 0, "log_b": 0, "saturation_delta": 0, "retouch_delta": 0}})
	if err != nil {
		t.Fatal(err)
	}
	sha, _ := canonical.Hash(json.RawMessage(body))
	recipe := SnapshotRecipe{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}, Body: body}
	id := uuid.New()
	snapshot, raw, err := CompileTargetSnapshot(id, inventory, []SnapshotRecipe{recipe})
	if err != nil || snapshot.SnapshotID != id || snapshot.Items[0].Recipe != recipe.Ref || len(raw) == 0 {
		t.Fatal("valid recipe", err)
	}
	bad := recipe
	bad.Body = []byte(strings.Replace(string(body), strings.Repeat("b", 64), strings.Repeat("c", 64), 1))
	if _, _, err := CompileTargetSnapshot(id, inventory, []SnapshotRecipe{bad}); err == nil {
		t.Fatal("changed bytes accepted")
	}
	bad.Ref.SHA256, _ = canonical.Hash(json.RawMessage(bad.Body))
	if _, _, err := CompileTargetSnapshot(id, inventory, []SnapshotRecipe{bad}); err == nil {
		t.Fatal("foreign canvas accepted")
	}
	if _, _, err := CompileTargetSnapshot(id, inventory, nil); err == nil {
		t.Fatal("partial snapshot accepted")
	}
}
