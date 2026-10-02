package v2learning

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestFrozenRecipePairBindsSourceStyleAndLocks(t *testing.T) {
	_, _, _, manifest := compiledTrainingDraftFixture(t)
	caseRef := manifest.Examples[0].RenderCase
	recipe := FrozenRecipe{SchemaVersion: 1, Source: caseRef.Binding,
		BaseStyle: v2input.Ref{ArtifactID: uuid.New(),
			SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		Renderer: manifest.RendererVersion, ICCLockSHA: manifest.ICCLockSHA,
		ModelLockSHA: manifest.ModelLockSHA}
	base, err := canonical.Marshal(recipe)
	if err != nil {
		t.Fatal(err)
	}
	style, err := ValidateFrozenRecipePair(base, base, caseRef, manifest)
	if err != nil || style != recipe.BaseStyle {
		t.Fatalf("valid frozen pair rejected: %v", err)
	}
	changed := recipe
	changed.BaseStyle.ArtifactID = uuid.New()
	approved, _ := canonical.Marshal(changed)
	if _, err := ValidateFrozenRecipePair(base, approved, caseRef, manifest); err == nil {
		t.Fatal("different base style accepted")
	}
	changed = recipe
	changed.Source.AssetVersionID = uuid.New()
	approved, _ = canonical.Marshal(changed)
	if _, err := ValidateFrozenRecipePair(base, approved, caseRef, manifest); err == nil {
		t.Fatal("foreign source accepted")
	}
	changed = recipe
	changed.ModelLockSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	approved, _ = canonical.Marshal(changed)
	if _, err := ValidateFrozenRecipePair(base, approved, caseRef, manifest); err == nil {
		t.Fatal("different model lock accepted")
	}
	var fields map[string]any
	if err := json.Unmarshal(base, &fields); err != nil {
		t.Fatal(err)
	}
	fields["unexpected"] = true
	unknown, _ := canonical.Marshal(fields)
	if _, err := DecodeFrozenRecipe(unknown); err == nil {
		t.Fatal("unknown recipe field accepted")
	}
	fields = map[string]any{}
	if err := json.Unmarshal(base, &fields); err != nil {
		t.Fatal(err)
	}
	delete(fields, "effective_delta")
	missing, _ := canonical.Marshal(fields)
	if _, err := DecodeFrozenRecipe(missing); err == nil {
		t.Fatal("missing delta accepted")
	}
}
