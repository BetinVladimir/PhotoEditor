package semanticreview

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestRemovalPreviewImageChangesOnlyPinnedPixelReference(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []json.RawMessage
	if json.Unmarshal(raw, &fixtures) != nil {
		t.Fatal("fixture invalid")
	}
	source := fixtures[0]
	image, err := review.ParseWorkingImage(source)
	if err != nil {
		t.Fatal(err)
	}
	hashRef := func(body []byte) v2input.Ref {
		sum := sha256.Sum256(body)
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	}
	p := RemovalPreviewParams{ExposurePreviewParams: previewParamsFixture(), Kind: "remove_object", ModelID: "big_lama", ContextPixels: 64, FeatherPixels: 8}
	p.Source.Source = hashRef(source)
	p.Source.CanvasHash, _ = canonical.Hash(image.Canvas)
	for _, mutation := range []string{"valid", "color", "pixels", "source", "roi"} {
		var output map[string]any
		json.Unmarshal(source, &output)
		pixels := v2input.Ref{ArtifactID: uuid.New(), SHA256: p.Plan.SHA256}
		output["pixels"] = pixels
		if mutation == "color" {
			output["color"] = map[string]any{"profile_id": "wrong", "encoding": "gamma24"}
		}
		body, _ := canonical.Marshal(output)
		e := RemovalPreviewEvidence{SchemaVersion: 1, Params: p, WorkingImage: hashRef(body), Pixels: pixels, Algorithm: "big_lama_work_srgb_delta_v1", ProtectedPixelsUnchanged: true, OutsideMaskUnchanged: true, ROI: []int64{0, 0, 1, 1}}
		if mutation == "pixels" {
			e.Pixels.ArtifactID = uuid.New()
		}
		if mutation == "roi" {
			e.ROI = []int64{0, 0, 9007199254740991, 1}
		}
		src := source
		if mutation == "source" {
			src = append(append([]byte(nil), source...), ' ')
		}
		_, err := VerifyRemovalPreviewImageBinding(e, src, body)
		if (err == nil) != (mutation == "valid") {
			t.Fatal(mutation, err)
		}
	}
}
