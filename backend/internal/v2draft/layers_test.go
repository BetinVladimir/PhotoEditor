package v2draft

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestValidateLayerStackPinsCanvasAndNestedRefs(t *testing.T) {
	ref := func() v2input.Ref {
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	}
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(),
		Source: ref(), CanvasHash: strings.Repeat("b", 64)}
	params, mask := ref(), ref()
	doc := map[string]any{"schema_version": 1, "source": binding,
		"layers": []any{map[string]any{"id": uuid.New(), "enabled": true,
			"opacity": 0.5, "blend": "normal", "operation": "adjust.exposure",
			"operation_version": "1", "params": params,
			"mask": map[string]any{"op": "invert", "operand": map[string]any{
				"op": "ref", "mask": mask, "canvas_hash": binding.CanvasHash}},
		}},
	}
	raw, err := canonical.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := ValidateLayerStack(raw, binding)
	if err != nil || len(refs) != 3 || refs[0].Ref != binding.Source ||
		refs[1].Ref != params || refs[2].Ref != mask || refs[2].Contract != "MaskRef" {
		t.Fatalf("invalid layer refs: %+v, %v", refs, err)
	}
	parent := ref()
	children, e := DocumentChildren(raw, "LayerStack", parent)
	if e != nil || len(children) != len(refs) {
		t.Fatal("document layer children", e)
	}
	for i, child := range children {
		if child.Ref != refs[i].Ref || child.Contract != refs[i].Contract || child.ParentRef != parent ||
			child.Path != "Document"+strings.TrimPrefix(refs[i].Path, "LayerStack") {
			t.Fatal("layer occurrence or parent lost", child)
		}
	}
	binding.CanvasHash = strings.Repeat("c", 64)
	if _, err := ValidateLayerStack(raw, binding); err == nil {
		t.Fatal("accepted layer stack bound to another canvas")
	}
	binding.CanvasHash = strings.Repeat("b", 64)
	deep := any(map[string]any{"op": "ref", "mask": mask,
		"canvas_hash": binding.CanvasHash})
	for i := 0; i < 17; i++ {
		deep = map[string]any{"op": "invert", "operand": deep}
	}
	doc["layers"].([]any)[0].(map[string]any)["mask"] = deep
	raw, err = canonical.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateLayerStack(raw, binding); err == nil {
		t.Fatal("accepted mask expression deeper than 16")
	}
	if _, err := DocumentChildren(raw, "LayerStack", parent); err == nil {
		t.Fatal("document reader accepted deep layer mask")
	}
}

func TestDocumentEmptyLayerStackRejectsMalformedSourceCanvas(t *testing.T) {
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref, CanvasHash: strings.Repeat("b", 64)}
	for _, canvas := range []string{strings.Repeat("b", 64), strings.Repeat("x", 64)} {
		binding.CanvasHash = canvas
		raw, err := canonical.Marshal(map[string]any{"schema_version": 1, "source": binding, "layers": []any{}})
		if err != nil {
			t.Fatal(err)
		}
		children, err := DocumentChildren(raw, "LayerStack", ref)
		if canvas == strings.Repeat("b", 64) {
			if err != nil || len(children) != 1 || children[0].Contract != "WorkingImage" || children[0].Ref != ref {
				t.Fatal("valid empty stack source lost", err)
			}
		} else if err == nil {
			t.Fatal("malformed canvas accepted")
		}
	}
}

func TestLayerStackRejectsNullDuplicateAndGeometryAfterPhotometric(t *testing.T) {
	ref := func() v2input.Ref { return v2input.Ref{uuid.New(), strings.Repeat("a", 64)} }
	binding := v2input.SessionVersionBinding{uuid.New(), ref(), strings.Repeat("b", 64)}
	layer := func(operation string) map[string]any {
		return map[string]any{"id": uuid.New(), "enabled": true, "opacity": 1, "blend": "normal", "operation": operation, "operation_version": "1", "params": ref(), "mask": map[string]any{"op": "ref", "mask": ref(), "canvas_hash": binding.CanvasHash}}
	}
	validate := func(layers ...any) error {
		raw, err := canonical.Marshal(map[string]any{"schema_version": 1, "source": binding, "layers": layers})
		if err != nil {
			t.Fatal(err)
		}
		_, err = ValidateLayerStack(raw, binding)
		return err
	}
	if err := validate(layer("optical.lens"), layer("adjust.exposure")); err != nil {
		t.Fatal("valid geometry prefix", err)
	}
	for _, operation := range []string{"optical.lens", "optical.perspective", "optical.panorama", "optical.focus_stack"} {
		t.Run(operation, func(t *testing.T) {
			if err := validate(layer("adjust.exposure"), layer(operation)); err == nil {
				t.Fatal("geometry after photometric accepted")
			}
		})
	}
	disabled := layer("optical.lens")
	disabled["enabled"] = false
	if err := validate(layer("adjust.exposure"), disabled); err != nil {
		t.Fatal("disabled geometry affected prefix", err)
	}
	disabled["enabled"] = true
	disabled["opacity"] = 0
	if err := validate(layer("adjust.exposure"), disabled); err != nil {
		t.Fatal("zero-opacity geometry affected prefix", err)
	}
	for _, field := range []string{"enabled", "opacity"} {
		t.Run("null "+field, func(t *testing.T) {
			bad := layer("adjust.exposure")
			bad[field] = nil
			if err := validate(bad); err == nil {
				t.Fatal("null scalar accepted")
			}
		})
	}
	first, second := layer("adjust.exposure"), layer("adjust.exposure")
	second["id"] = first["id"]
	if err := validate(first, second); err == nil {
		t.Fatal("duplicate layer identity accepted")
	}
	raw, _ := canonical.Marshal(map[string]any{"schema_version": 1, "source": binding, "layers": []any{first}})
	duplicate := strings.Replace(string(raw), `"enabled":true`, `"enabled":true,"enabled":false`, 1)
	if _, err := ValidateLayerStack([]byte(duplicate), binding); err == nil {
		t.Fatal("duplicate JSON key accepted")
	}
}
