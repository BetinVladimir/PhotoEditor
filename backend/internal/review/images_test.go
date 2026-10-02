package review

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"photoeditor/backend/internal/canonical"
)

func imageFixtures(t *testing.T) []json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile("testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if err = json.Unmarshal(raw, &images); err != nil {
		t.Fatal(err)
	}
	return images
}
func TestWorkingImagePythonTransformParity(t *testing.T) {
	for _, raw := range imageFixtures(t) {
		image, err := ParseWorkingImage(raw)
		if err != nil {
			t.Fatal(err)
		}
		source := image.Source(image.Pixels)
		source.TransformChain[0] = 'x'
		if image.TransformChain[0] == 'x' {
			t.Fatal("source aliases validated chain bytes")
		}
	}
}

func TestLastTransformStepUsesValidatedChain(t *testing.T) {
	image, err := ParseWorkingImage(imageFixtures(t)[2])
	if err != nil {
		t.Fatal(err)
	}
	step, err := LastTransformStep(image)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if json.Unmarshal(step, &value) != nil || value["kind"] != "dense_warp" || value["inverse_field"] == nil {
		t.Fatalf("step=%s", step)
	}
	identity, _ := ParseWorkingImage(imageFixtures(t)[0])
	if _, err = LastTransformStep(identity); err == nil {
		t.Fatal("empty chain accepted")
	}
}
func TestWorkingImageRejectsInvalidGeometry(t *testing.T) {
	images := imageFixtures(t)
	cases := []struct {
		name   string
		index  int
		change func(map[string]any)
	}{
		{"singular", 1, func(d map[string]any) { step(d)["forward_matrix"] = []float64{0, 0, 0, 0, 0, 0, 0, 0, 1} }},
		{"horizon", 1, func(d map[string]any) { step(d)["forward_matrix"] = []float64{1, 0, 0, 0, 1, 0, .01, 0, -1} }},
		{"changed source", 1, func(d map[string]any) {
			step(d)["from_canvas"].(map[string]any)["source_sha256"] = strings.Repeat("a", 64)
		}},
		{"crop size", 3, func(d map[string]any) { step(d)["crop_rect"].(map[string]any)["width"] = 300 }},
		{"crop bounds", 3, func(d map[string]any) { step(d)["crop_rect"].(map[string]any)["x"] = 399 }},
		{"resize sampling", 4, func(d map[string]any) { step(d)["pixel_centers"] = "align_corners" }},
		{"dense field", 2, func(d map[string]any) { step(d)["inverse_field"].(map[string]any)["sha256"] = "wrong" }},
		{"mixed union", 1, func(d map[string]any) { step(d)["pixel_centers"] = "half_pixel" }},
		{"target mismatch", 1, func(d map[string]any) { d["canvas"].(map[string]any)["space_id"] = "other" }},
		{"discontinuous", 1, func(d map[string]any) {
			raw, _ := json.Marshal(step(d))
			var second map[string]any
			json.Unmarshal(raw, &second)
			second["from_canvas"].(map[string]any)["space_id"] = "other"
			d["transform_chain"].(map[string]any)["steps"] = []any{step(d), second}
		}},
		{"missing alpha", 0, func(d map[string]any) { delete(d, "alpha") }},
		{"invalid encoding", 0, func(d map[string]any) { d["color"].(map[string]any)["encoding"] = "linear" }},
		{"pixel cap", 0, func(d map[string]any) { d["canvas"].(map[string]any)["width"] = 1000000 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var doc map[string]any
			json.Unmarshal(images[c.index], &doc)
			c.change(doc)
			chain := doc["transform_chain"].(map[string]any)
			hash, err := canonical.Hash(map[string]any{"steps": chain["steps"]})
			if err != nil {
				t.Fatal(err)
			}
			chain["hash"] = hash
			raw, err := canonical.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = ParseWorkingImage(raw); err == nil {
				t.Fatal("invalid geometry accepted")
			}
		})
	}
}

func TestParseMaskRefRequiresExactCanvasAndModelLock(t *testing.T) {
	var image map[string]any
	if err := json.Unmarshal(imageFixtures(t)[0], &image); err != nil {
		t.Fatal(err)
	}
	doc := map[string]any{
		"artifact": map[string]any{
			"artifact_id": "00000000-0000-4000-8000-000000000001",
			"sha256":      strings.Repeat("1", 64),
		},
		"canvas":          image["canvas"],
		"transform_chain": image["transform_chain"],
		"object_id":       nil,
		"selector":        "person",
		"confidence":      0.75,
		"model_lock_id":   strings.Repeat("2", 64),
		"protected":       false,
	}
	raw, err := canonical.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	mask, err := ParseMaskRef(raw)
	if err != nil {
		t.Fatal(err)
	}
	if mask.Selector != "person" || mask.Confidence != 0.75 || mask.ModelLockID == nil {
		t.Fatal("valid mask metadata was not preserved")
	}

	cases := []struct {
		name   string
		change func(map[string]any)
	}{
		{"unknown field", func(d map[string]any) { d["extra"] = true }},
		{"invalid confidence", func(d map[string]any) { d["confidence"] = 1.01 }},
		{"invalid selector", func(d map[string]any) { d["selector"] = "" }},
		{"invalid model lock", func(d map[string]any) { d["model_lock_id"] = "latest" }},
		{"invalid canvas", func(d map[string]any) { d["canvas"].(map[string]any)["orientation"] = 2 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var changed map[string]any
			if err := json.Unmarshal(raw, &changed); err != nil {
				t.Fatal(err)
			}
			c.change(changed)
			changedRaw, err := canonical.Marshal(changed)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = ParseMaskRef(changedRaw); err == nil {
				t.Fatal("invalid mask ref accepted")
			}
		})
	}
}
func step(d map[string]any) map[string]any {
	return d["transform_chain"].(map[string]any)["steps"].([]any)[0].(map[string]any)
}

func TestDenseWarpHalfPixelMarker(t *testing.T) {
	for _, marker := range []string{"half_pixel", "align_corners"} {
		var doc map[string]any
		if err := json.Unmarshal(imageFixtures(t)[2], &doc); err != nil {
			t.Fatal(err)
		}
		step(doc)["pixel_centers"] = marker
		chain := doc["transform_chain"].(map[string]any)
		digest, err := canonical.Hash(map[string]any{"steps": chain["steps"]})
		if err != nil {
			t.Fatal(err)
		}
		chain["hash"] = digest
		raw, err := canonical.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		_, err = ParseWorkingImage(raw)
		if (marker == "half_pixel") != (err == nil) {
			t.Fatalf("marker=%s error=%v", marker, err)
		}
	}
}
