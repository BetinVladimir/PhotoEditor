package masknpy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image/png"
	"strings"
	"testing"
)

func TestProbabilityMaskPreviewRequiresVerifiedWholeNPY(t *testing.T) {
	raw := arrayFixture("(2,3)", []float32{0, 0.5, 1, 1, 0.5, 0})
	digest := sha256.Sum256(raw)
	sha := hex.EncodeToString(digest[:])
	preview, err := RenderProbabilityMaskPreview(context.Background(), bytes.NewReader(raw), int64(len(raw)), sha, 3, 2, 64)
	if err != nil {
		t.Fatal(err)
	}
	image, err := png.Decode(bytes.NewReader(preview))
	if err != nil || image.Bounds().Dx() != 3 || image.Bounds().Dy() != 2 {
		t.Fatal("invalid preview", err)
	}
	_, _, _, alpha0 := image.At(0, 0).RGBA()
	_, _, _, alpha1 := image.At(2, 0).RGBA()
	if alpha0 >= alpha1 {
		t.Fatal("mask opacity was lost")
	}
	if output, err := RenderProbabilityMaskPreview(context.Background(), bytes.NewReader(raw), int64(len(raw)), strings.Repeat("0", 64), 3, 2, 64); err == nil || output != nil {
		t.Fatal("accepted substituted source")
	}
	if output, err := RenderProbabilityMaskPreview(context.Background(), bytes.NewReader(raw[:len(raw)-1]), int64(len(raw)), sha, 3, 2, 64); err == nil || output != nil {
		t.Fatal("accepted truncated source")
	}
}
