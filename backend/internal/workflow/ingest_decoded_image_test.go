package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
)

func decodedFixture(t *testing.T) (review.WorkingImage, DecodeOutputBinding, IngestMetadata, string) {
	t.Helper()
	source := review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	pixels := review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("b", 64)}
	chain := json.RawMessage(`{"hash":"4430e7786edc0f8419f02e909c15422ebf572287a58132d8f6f33250ce053121","steps":[]}`)
	icc := strings.Repeat("c", 64)
	img := review.WorkingImage{Pixels: pixels, Canvas: review.Canvas{Width: 400, Height: 300, Orientation: 1, SpaceID: "oriented:" + source.SHA256, SourceSHA256: source.SHA256}, Color: review.ColorSpace{ProfileID: "work_rec2020_gamma24_v1", Encoding: "gamma24", ICCSHA256: icc}, DType: "float32", TransformChain: chain}
	return img, DecodeOutputBinding{Pixels: pixels}, IngestMetadata{Source: source, Width: 400, Height: 300, Orientation: 1, Format: "jpeg"}, icc
}
func TestReadDecodedImageOrientationAndAlpha(t *testing.T) {
	for orientation := 1; orientation <= 8; orientation++ {
		for _, alpha := range []bool{false, true} {
			img, refs, meta, icc := decodedFixture(t)
			meta.Orientation = orientation
			meta.HasAlpha = alpha
			if orientation >= 5 {
				img.Canvas.Width, img.Canvas.Height = img.Canvas.Height, img.Canvas.Width
			}
			if alpha {
				ref := review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("d", 64)}
				img.Alpha = &ref
				refs.Alpha = &ref
				meta.Format = "png"
			}
			raw, _ := canonical.Marshal(img)
			refs.Manifest = metadataRef(raw)
			got, err := ReadDecodedImage(context.Background(), bytes.NewReader(raw), refs, meta, icc)
			if err != nil || got.Canvas != img.Canvas {
				t.Fatal("valid decode rejected", orientation, alpha, err)
			}
		}
	}
}
func TestReadDecodedImageRejectsReboundOutputs(t *testing.T) {
	cases := map[string]func(*review.WorkingImage){
		"pixels":        func(i *review.WorkingImage) { i.Pixels.ID = uuid.New() },
		"source":        func(i *review.WorkingImage) { i.Canvas.SourceSHA256 = strings.Repeat("e", 64) },
		"space":         func(i *review.WorkingImage) { i.Canvas.SpaceID = "other" },
		"dimensions":    func(i *review.WorkingImage) { i.Canvas.Width = 300; i.Canvas.Height = 400 },
		"dtype":         func(i *review.WorkingImage) { i.DType = "uint16" },
		"profile bytes": func(i *review.WorkingImage) { i.Color.ICCSHA256 = strings.Repeat("e", 64) },
		"profile":       func(i *review.WorkingImage) { i.Color.ProfileID = "linear_srgb_v1"; i.Color.Encoding = "linear" },
		"unexpected alpha": func(i *review.WorkingImage) {
			i.Alpha = &review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("e", 64)}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			img, refs, meta, icc := decodedFixture(t)
			mutate(&img)
			raw, _ := canonical.Marshal(img)
			refs.Manifest = metadataRef(raw)
			if _, err := ReadDecodedImage(context.Background(), bytes.NewReader(raw), refs, meta, icc); err == nil {
				t.Fatal("foreign decode accepted")
			}
		})
	}
}
func TestReadDecodedImageHashCancellationAndRAWDimensions(t *testing.T) {
	img, refs, meta, icc := decodedFixture(t)
	meta.Format = "raw"
	img.Canvas.Width, img.Canvas.Height = 300, 400
	raw, _ := canonical.Marshal(img)
	refs.Manifest = metadataRef(raw)
	if _, err := ReadDecodedImage(context.Background(), bytes.NewReader(raw), refs, meta, icc); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadDecodedImage(context.Background(), bytes.NewReader(append(raw, ' ')), refs, meta, icc); err == nil {
		t.Fatal("changed bytes accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadDecodedImage(ctx, bytes.NewReader(raw), refs, meta, icc); err != context.Canceled {
		t.Fatal("cancellation lost")
	}
	img.Canvas.Width, img.Canvas.Height = 600, 200
	raw, _ = canonical.Marshal(img)
	refs.Manifest = metadataRef(raw)
	if _, err := ReadDecodedImage(context.Background(), bytes.NewReader(raw), refs, meta, icc); err == nil {
		t.Fatal("arbitrary equal area accepted")
	}
}
