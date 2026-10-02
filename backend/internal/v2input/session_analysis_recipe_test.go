package v2input

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
)

func TestSessionAnalysisRecipeBindsExactSourceAndVersionedBytes(t *testing.T) {
	binding := SessionVersionBinding{AssetVersionID: uuid.New(),
		Source:     Ref{uuid.New(), strings.Repeat("a", 64)},
		CanvasHash: strings.Repeat("b", 64)}
	base := Ref{uuid.New(), strings.Repeat("c", 64)}
	recipe := sessionRecipe{SchemaVersion: 3, Source: binding,
		BaseStyle: base, Overrides: []Ref{}, RendererVersion: "v2.2"}
	check := func(recipe sessionRecipe, valid bool) {
		raw, err := canonical.Marshal(recipe)
		if err != nil {
			t.Fatal(err)
		}
		sha := sha256.Sum256(raw)
		ref := Ref{uuid.New(), hex.EncodeToString(sha[:])}
		object := objectstore.Object{Key: "recipe", VersionID: "v1",
			SHA256: ref.SHA256, Bytes: int64(len(raw)),
			ContentType: "application/json"}
		err = readSessionRecipe(context.Background(), sessionImageBytes(raw),
			object, ref, binding)
		if (err == nil) != valid {
			t.Fatalf("valid=%v: %v", valid, err)
		}
	}
	check(recipe, true)
	recipe.ProtectedMask = &Ref{uuid.New(), strings.Repeat("d", 64)}
	check(recipe, true)
	recipe.ProtectedMask = nil
	validBody, err := canonical.Marshal(recipe)
	if err != nil {
		t.Fatal(err)
	}
	if validateSessionRecipe(append(append([]byte(nil), validBody[:len(validBody)-1]...),
		[]byte(`,"unexpected":1}`)...), binding) == nil {
		t.Fatal("accepted an extra recipe field")
	}
	var nested map[string]any
	if err := json.Unmarshal(validBody, &nested); err != nil {
		t.Fatal(err)
	}
	nested["source"].(map[string]any)["source"].(map[string]any)["extra"] = true
	withExtra, err := canonical.Marshal(nested)
	if err != nil {
		t.Fatal(err)
	}
	if validateSessionRecipe(withExtra, binding) == nil {
		t.Fatal("accepted an extra source ref field")
	}
	nested["source"] = map[string]any{"asset_version_id": binding.AssetVersionID,
		"source": binding.Source, "canvas_hash": binding.CanvasHash}
	nested["protected_mask"] = nil
	withNull, err := canonical.Marshal(nested)
	if err != nil || validateSessionRecipe(withNull, binding) == nil {
		t.Fatal("accepted null protected mask")
	}
	recipe.Source.AssetVersionID = uuid.New()
	check(recipe, false)
	recipe.Source = binding
	recipe.ParameterDelta.EV = 1.01
	check(recipe, false)
	recipe.ParameterDelta.EV = 0
	raw, err := canonical.Marshal(recipe)
	if err != nil {
		t.Fatal(err)
	}
	sha := sha256.Sum256(raw)
	ref := Ref{uuid.New(), hex.EncodeToString(sha[:])}
	object := objectstore.Object{Key: "recipe", VersionID: "v1",
		SHA256: ref.SHA256, Bytes: int64(len(raw)),
		ContentType: "application/json"}
	changed := append([]byte(nil), raw...)
	changed[len(changed)-2] = '0'
	if err := readSessionRecipe(context.Background(), sessionImageBytes(changed),
		object, ref, binding); err == nil {
		t.Fatal("accepted altered object bytes")
	}
}
