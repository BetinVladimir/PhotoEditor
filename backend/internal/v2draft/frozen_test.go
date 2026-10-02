package v2draft

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestFrozenRecipeBoundsAndLocks(t *testing.T) {
	raw, err := os.ReadFile("../run/identity_style.json")
	if err != nil {
		t.Fatal(err)
	}
	base, err := DecodeStyleBaseline(raw)
	if err != nil || base.GainsRGB != [3]float64{1, 1, 1} || base.Saturation != 1 {
		t.Fatalf("failed to read scalar baseline: %+v, %v", base, err)
	}
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(),
		Source:     v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)},
		CanvasHash: strings.Repeat("b", 64)}
	style := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}
	delta := CorrectionVector{EV: 0.5, LogR: 0.1}
	frozen, err := BuildFrozenRecipe(binding, style, base, delta,
		strings.Repeat("d", 64), strings.Repeat("e", 64))
	if err != nil {
		t.Fatal(err)
	}
	var parsed FrozenRecipe
	if json.Unmarshal(frozen, &parsed) != nil || parsed.SchemaVersion != 1 ||
		parsed.Source != binding || parsed.EffectiveDelta != delta ||
		parsed.RendererVersion != "v2.2" {
		t.Fatalf("invalid frozen recipe: %s", frozen)
	}
	base.GainsRGB[0] = 3.5
	if _, err = BuildFrozenRecipe(binding, style, base, delta,
		strings.Repeat("d", 64), strings.Repeat("e", 64)); err == nil {
		t.Fatal("accepted effective gain above StyleV2 bound")
	}
	base.GainsRGB[0] = 1
	if _, err = BuildFrozenRecipe(binding, style, base, delta,
		"latest", strings.Repeat("e", 64)); err == nil {
		t.Fatal("accepted an unpinned ICC lock")
	}
}
