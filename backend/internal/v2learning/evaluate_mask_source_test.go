package v2learning

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
)

func TestParseFrozenProtectedMaskRequiresSameCanvasAndProtection(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []json.RawMessage
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(fixtures[0])
	if err != nil {
		t.Fatal(err)
	}
	doc := map[string]any{
		"artifact": map[string]any{"artifact_id": "00000000-0000-4000-8000-000000000001",
			"sha256": strings.Repeat("1", 64)},
		"canvas": image.Canvas, "transform_chain": json.RawMessage(image.TransformChain),
		"object_id": nil, "selector": "manual", "confidence": 1.0,
		"model_lock_id": nil, "protected": true,
	}
	body, err := canonical.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := ParseFrozenProtectedMask(body, image)
	if err != nil || ref.SHA256 != strings.Repeat("1", 64) {
		t.Fatalf("valid protected mask rejected: %v", err)
	}
	doc["protected"] = false
	body, _ = canonical.Marshal(doc)
	if _, err := ParseFrozenProtectedMask(body, image); err == nil {
		t.Fatal("unprotected mask accepted")
	}
	doc["protected"] = true
	changed := image.Canvas
	changed.Width++
	doc["canvas"] = changed
	body, _ = canonical.Marshal(doc)
	if _, err := ParseFrozenProtectedMask(body, image); err == nil {
		t.Fatal("foreign canvas mask accepted")
	}
}
