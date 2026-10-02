package v2learning

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func TestParseFrozenEvaluationImagePinsCanvasAndWorkingColor(t *testing.T) {
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
	canvasSHA, err := canonical.Hash(image.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(),
		CanvasHash: canvasSHA}
	got, err := ParseFrozenEvaluationImage(fixtures[0], binding)
	if err != nil || got.Pixels != image.Pixels {
		t.Fatalf("qualified frozen image rejected: %v", err)
	}
	binding.CanvasHash = strings.Repeat("0", 64)
	if _, err := ParseFrozenEvaluationImage(fixtures[0], binding); err == nil {
		t.Fatal("image from another canvas accepted")
	}
}
