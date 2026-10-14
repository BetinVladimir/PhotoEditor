package v2render

import (
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
)

func TestPerspectiveResolvedTransformWorkerABI(t *testing.T) {
	doc := map[string]any{"schema_version": 1, "from_canvas_hash": strings.Repeat("a", 64), "to_canvas_hash": strings.Repeat("b", 64), "kind": "homography", "matrix": []float64{1, 0, 0, 0, 1, 0, 0, 0, 1}}
	raw, err := canonical.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePerspectiveResolvedTransform(raw); err != nil {
		t.Fatal(err)
	}
	for _, matrix := range []any{[]float64{1, 0}, [][]float64{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}, []float64{1, 0, 0, 0, 0, 0, 0, 0, 1}, []float64{1e9, 0, 0, 0, 1, 0, 0, 0, 1}} {
		doc["matrix"] = matrix
		raw, err = canonical.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		if err := validatePerspectiveResolvedTransform(raw); err == nil {
			t.Fatal("invalid matrix accepted", matrix)
		}
	}
}
