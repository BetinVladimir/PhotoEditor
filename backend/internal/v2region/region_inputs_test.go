package v2region

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2learning"
	"testing"
)

func TestRegionRequirementsIncludeAlphaAndDenseGeometry(t *testing.T) {
	req, recipeRaw, sourceRef, sourceRaw := regionEvidenceFixture(t)
	source, err := review.ParseWorkingImage(sourceRaw)
	if err != nil {
		t.Fatal(err)
	}
	alpha, field := v2input.Ref{ArtifactID: uuid.New(), SHA256: regionSHA}, v2input.Ref{ArtifactID: uuid.New(), SHA256: regionSHA}
	source.Alpha = &review.ArtifactRef{ID: alpha.ArtifactID, SHA256: alpha.SHA256}
	steps := []any{map[string]any{"kind": "dense_warp", "from_canvas": source.Canvas, "to_canvas": source.Canvas, "invertibility": "partial", "valid_region": map[string]any{"x": 0, "y": 0, "width": 2000, "height": 1000}, "inverse_field": field}}
	chainHash, _ := canonical.Hash(map[string]any{"steps": steps})
	source.TransformChain, _ = canonical.Marshal(map[string]any{"steps": steps, "hash": chainHash})
	sourceRaw, _ = canonical.Marshal(source)
	sourceRef.SHA256, _ = canonical.Hash(source)
	var recipe v2learning.FrozenRecipe
	if json.Unmarshal(recipeRaw, &recipe) != nil {
		t.Fatal("fixture recipe")
	}
	recipe.Source.Source = sourceRef
	recipeRaw, _ = canonical.Marshal(recipe)
	req.Recipe.SHA256, _ = canonical.Hash(recipe)
	evidence, err := CompileRegionEvidence(req, recipeRaw, sourceRef, sourceRaw)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := regionInputRequirements(sourceRef, v2input.Ref{ArtifactID: req.Recipe.ArtifactID, SHA256: req.Recipe.SHA256}, evidence)
	if err != nil || len(entries) != 6 {
		t.Fatalf("missing inputs: %v %v", entries, err)
	}
	seen := map[uuid.UUID]string{}
	for _, entry := range entries {
		seen[entry.Ref.ArtifactID] = entry.Contract
	}
	if seen[alpha.ArtifactID] != "NpyArray" || seen[field.ArtifactID] != "NpyArray" || seen[recipe.BaseStyle.ArtifactID] != "StyleV2" {
		t.Fatal("omitted alpha, warp or style")
	}
	// A UUID cannot acquire two contracts in the same worker inventory.
	evidence.Recipe.BaseStyle = sourceRef
	if _, err := regionInputRequirements(sourceRef, v2input.Ref{ArtifactID: req.Recipe.ArtifactID, SHA256: req.Recipe.SHA256}, evidence); err == nil {
		t.Fatal("ambiguous artifact contract accepted")
	}
}
