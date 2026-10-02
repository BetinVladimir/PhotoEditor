package v2input

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
)

func TestLensProfileStrictContract(t *testing.T) {
	valid := map[string]any{"schema_version": 1, "camera_model": "Camera", "lens_model": "Lens", "focal_mm": 50, "aperture": 2,
		"cx": .5, "cy": .5, "k": []any{0, 0, 0}, "p": []any{0, 0}, "vignette": []any{0, 0, 0}, "ca_red_scale": 1, "ca_blue_scale": 1,
		"qualification": Ref{uuid.New(), strings.Repeat("a", 64)}}
	raw, _ := canonical.Marshal(valid)
	if _, err := ParseLensProfile(raw); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]any{"k": []any{nil, 0, 0}, "p": []any{.2, 0}, "vignette": []any{0, 0}, "cx": nil,
		"camera_model": strings.Repeat("x", 121), "ca_red_scale": 1.02, "unexpected": true} {
		changed := map[string]any{}
		for k, v := range valid {
			changed[k] = v
		}
		changed[key] = value
		body, _ := canonical.Marshal(changed)
		if _, err := ParseLensProfile(body); err == nil {
			t.Fatal("invalid field accepted", key)
		}
	}
	delete(valid, "cx")
	raw, _ = canonical.Marshal(valid)
	if _, err := ParseLensProfile(raw); err == nil {
		t.Fatal("missing center accepted")
	}
}
