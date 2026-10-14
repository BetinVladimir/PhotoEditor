package interchange

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestImportOverrideStyleSourceBindsExactArchivedRecipe(t *testing.T) {
	parsed := importParsedValidatorFixture()
	old := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("working"))}, CanvasHash: rawSHA([]byte("canvas"))}
	style := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("style"))}
	override := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("override"))}
	second := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("second override"))}
	recipe, err := canonical.Marshal(map[string]any{
		"schema_version": 3, "source": old, "base_style": style,
		"session_corrections": nil, "learning_version_id": nil,
		"overrides": []v2input.Ref{override, second}, "layer_stack": nil,
		"renderer_version": "v2.2", "parameter_delta": map[string]any{
			"ev": 0, "log_r": 0, "log_g": 0, "log_b": 0,
			"saturation_delta": 0, "retouch_delta": 0,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	recipeSHA := rawSHA(recipe)
	path := "recipes/" + uuid.New().String() + ".json"
	parsed.Manifest.Entries[0].RecipePath = &path
	parsed.Manifest.Entries[0].RecipeSHA = &recipeSHA
	parsed.Manifest.Inventory = append(parsed.Manifest.Inventory,
		importBundleInventory{ID: uuid.New(), Path: path, SHA: recipeSHA, Contract: "EditingRecipeV3"},
		importBundleInventory{ID: style.ArtifactID, Path: "dependencies/" + style.ArtifactID.String() + ".json", SHA: style.SHA256, Contract: "StyleV2"},
		importBundleInventory{ID: override.ArtifactID, Path: "dependencies/" + override.ArtifactID.String() + ".json", SHA: override.SHA256, Contract: "Override"},
		importBundleInventory{ID: second.ArtifactID, Path: "dependencies/" + second.ArtifactID.String() + ".json", SHA: second.SHA256, Contract: "Override"},
	)
	stagedRecipe := v2input.Ref{ArtifactID: uuid.New(), SHA256: recipeSHA}
	parsed.Files = append(parsed.Files,
		importParsedFile{Path: path, Content: stagedRecipe, Contract: "EditingRecipeV3"},
		importParsedFile{Path: "dependencies/" + style.ArtifactID.String() + ".json", Content: v2input.Ref{ArtifactID: uuid.New(), SHA256: style.SHA256}, Contract: "StyleV2"},
		importParsedFile{Path: "dependencies/" + override.ArtifactID.String() + ".json", Content: v2input.Ref{ArtifactID: uuid.New(), SHA256: override.SHA256}, Contract: "Override"},
		importParsedFile{Path: "dependencies/" + second.ArtifactID.String() + ".json", Content: v2input.Ref{ArtifactID: uuid.New(), SHA256: second.SHA256}, Contract: "Override"},
	)
	parsedRaw, err := canonical.Marshal(parsed)
	if err != nil {
		t.Fatal(err)
	}
	archive := &sourceArchiveReader{expected: stagedRecipe, body: recipe}
	source := ImportOverrideStyleSource{Parsed: parsedRaw, Entry: parsed.Manifest.Entries[0].ID, Old: old, Reader: archive}
	actual, err := source.ReadBaseStyle(context.Background(), override)
	if err != nil || actual != style || archive.calls != 2 {
		t.Fatal("exact override/base-style relation unavailable", actual, err)
	}
	base, ordered, err := source.ReadComposition(context.Background(), override)
	if err != nil || base != style || len(ordered) != 2 || ordered[0] != override || ordered[1] != second || archive.calls != 4 {
		t.Fatal("archived override order unavailable", base, ordered, err)
	}
	if actual, err := source.ReadBaseStyle(context.Background(), v2input.Ref{ArtifactID: uuid.New(), SHA256: override.SHA256}); err == nil || actual != (v2input.Ref{}) || archive.calls != 4 {
		t.Fatal("foreign override reached archive IO", actual, err)
	}
	archive.second = []byte(`{}`)
	if actual, err := source.ReadBaseStyle(context.Background(), override); err == nil || actual != (v2input.Ref{}) {
		t.Fatal("changed archived recipe retained base-style authority", actual, err)
	}
}
