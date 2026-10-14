package v2optical

import (
	"context"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"testing"
)

func TestPerspectiveOriginalSourceBinding(t *testing.T) {
	raw, err := os.ReadFile("testdata/negative-source.json")
	if err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(raw)
	if err != nil {
		t.Fatal(err)
	}
	p := perspectiveIntent()
	p.Base.Source.SHA256, err = canonical.Hash(image)
	if err != nil {
		t.Fatal(err)
	}
	p.Base.CanvasHash, err = canonical.Hash(image.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = canonical.Marshal(image)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyPerspectiveWorkingImage(p, raw); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyPerspectiveWorkingImage(p, append(append([]byte{}, raw...), 32)); err == nil {
		t.Fatal("modified source bytes accepted")
	}
	p.Base.CanvasHash = p.Base.Source.SHA256
	if _, err := VerifyPerspectiveWorkingImage(p, raw); err == nil {
		t.Fatal("foreign canvas accepted")
	}
}

// Long, valid transform histories must survive the original-source reader even
// when their canonical metadata exceeds the old 64 KiB draft limit.
func TestPerspectiveOriginalSourceLongTransformHistory(t *testing.T) {
	raw, err := os.ReadFile("testdata/negative-source.json")
	if err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(raw)
	if err != nil {
		t.Fatal(err)
	}
	steps := make([]any, 128)
	for i := range steps {
		steps[i] = map[string]any{
			"kind": "matrix", "from_canvas": image.Canvas, "to_canvas": image.Canvas,
			"invertibility":  "invertible",
			"valid_region":   map[string]int{"x": 0, "y": 0, "width": 100, "height": 100},
			"forward_matrix": []float64{1, 0.000000000123456789, 0.000000000234567891, 0.000000000345678912, 1, 0.000000000456789123, 0.000000000567891234, 0.000000000678912345, 1},
		}
	}
	chainHash, err := canonical.Hash(map[string]any{"steps": steps})
	if err != nil {
		t.Fatal(err)
	}
	image.TransformChain, err = canonical.Marshal(map[string]any{"steps": steps, "hash": chainHash})
	if err != nil {
		t.Fatal(err)
	}
	raw, err = canonical.Marshal(image)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) <= 65536 || len(raw) > PerspectiveWorkingImageMaxBytes {
		t.Fatalf("fixture must exercise the expanded source bound: %d bytes", len(raw))
	}
	p := perspectiveIntent()
	p.Base.Source.SHA256, err = canonical.Hash(image)
	if err != nil {
		t.Fatal(err)
	}
	p.Base.CanvasHash, err = canonical.Hash(image.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = VerifyPerspectiveWorkingImage(p, raw); err != nil {
		t.Fatal(err)
	}
	lines := PerspectiveLineSegments{SchemaVersion: 1, Lines: []PerspectiveLine{
		{A: PerspectivePoint{0, .2}, B: PerspectivePoint{1, .2}, Family: "horizontal"},
		{A: PerspectivePoint{0, .8}, B: PerspectivePoint{1, .8}, Family: "horizontal"},
		{A: PerspectivePoint{.2, 0}, B: PerspectivePoint{.2, 1}, Family: "vertical"},
		{A: PerspectivePoint{.8, 0}, B: PerspectivePoint{.8, 1}, Family: "vertical"},
	}}
	draft, err := CompilePerspectiveLinesDraft(context.Background(), auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), uuid.New(), uuid.New(), "long-source", p.Base, raw, lines)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := draft.Snapshot()
	if err != nil || snapshot.SourceJCS != string(raw) {
		t.Fatal("long source not preserved by publication draft", err)
	}
	changed := append([]byte(nil), raw...)
	changed[len(changed)-1] = ' '
	if _, err = VerifyPerspectiveWorkingImage(p, changed); err == nil {
		t.Fatal("changed long source accepted")
	}
	if _, err = VerifyPerspectiveWorkingImage(p, make([]byte, PerspectiveWorkingImageMaxBytes+1)); err == nil {
		t.Fatal("oversized source accepted")
	}
}
