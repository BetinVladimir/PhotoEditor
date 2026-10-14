package v2draft

import (
	"bytes"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestFrozenBackgroundMatchesPythonCanonicalContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/frozen-background.json")
	if err != nil {
		t.Fatal(err)
	}
	pin, err := DecodeFrozenBackgroundAsset(raw)
	if err != nil {
		t.Fatal(err)
	}
	asset := PortableBackgroundAsset{PortableID: pin.PortableAssetID, StyleVersionID: pin.StyleVersionID, Ref: pin.Source, Object: objectstore.Object{Bytes: pin.ExpectedBytes, SHA256: pin.Source.SHA256, ContentType: pin.MediaType, Key: "imported", VersionID: "v1"}}
	actual, err := FreezeBackgroundAsset(asset, pin.Style, pin.WorkICCSHA, nil)
	if err != nil || !bytes.Equal(actual, raw) {
		t.Fatalf("wire mismatch: %s %v", actual, err)
	}
	asset.Object.SHA256 = pin.Style.SHA256
	if _, err = FreezeBackgroundAsset(asset, pin.Style, pin.WorkICCSHA, nil); err == nil {
		t.Fatal("mismatched object pin accepted")
	}
}

func TestFrozenRenderBackgroundMustMatchStyleAndICC(t *testing.T) {
	raw, err := os.ReadFile("testdata/frozen-background.json")
	if err != nil {
		t.Fatal(err)
	}
	pin, err := DecodeFrozenBackgroundAsset(raw)
	if err != nil {
		t.Fatal(err)
	}
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("d", 64)}, CanvasHash: strings.Repeat("e", 64)}
	recipe, err := canonical.Marshal(map[string]any{"schema_version": 3, "source": binding, "base_style": pin.Style, "session_corrections": nil, "learning_version_id": nil, "overrides": []v2input.Ref{}, "layer_stack": nil, "renderer_version": "v2.2", "parameter_delta": CorrectionVector{}})
	if err != nil {
		t.Fatal(err)
	}
	inputs := ResolvedRecipeInputs{Binding: binding, BaseStyle: pin.Style, Style: StyleBaseline{GainsRGB: [3]float64{1, 1, 1}, Saturation: 1}, ICCLockSHA: pin.WorkICCSHA, ModelLockSHA: strings.Repeat("f", 64)}
	frozen, err := BuildFrozenRenderRecipeWithBackground(recipe, inputs, nil, raw)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeFrozenRenderRecipe(frozen)
	if err != nil || decoded.BackgroundAsset == nil || decoded.BackgroundAsset.Source != pin.Source {
		t.Fatal("background lost", err)
	}
	parent := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	children, err := renderRecipeRefs(frozen, "Params", parent)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, child := range children {
		if child.Contract != "StyleBackgroundAsset" {
			continue
		}
		found = true
		if child.Ref != pin.Source || child.ParentRef != parent || child.Path != "Params.background_asset.source" || child.ExpectedBackground == nil || child.ExpectedBackground.Style != pin.Style {
			t.Fatalf("background authority lost: %#v", child)
		}
		owned := ownOccurrenceNode(child)
		child.ExpectedBackground.PortableAssetID = "changed"
		if owned.ExpectedBackground.PortableAssetID != "studio" {
			t.Fatal("shared occurrence authority")
		}
	}
	if !found {
		t.Fatal("background missing from closure")
	}
	inputs.ICCLockSHA = strings.Repeat("e", 64)
	if _, err = BuildFrozenRenderRecipeWithBackground(recipe, inputs, nil, raw); err == nil {
		t.Fatal("foreign ICC pin accepted")
	}
}
