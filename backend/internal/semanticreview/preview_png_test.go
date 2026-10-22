package semanticreview

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestPreviewOverlayPNGPhysicalValidation(t *testing.T) {
	im := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	im.SetNRGBA(0, 0, color.NRGBA{255, 80, 40, 0})
	im.SetNRGBA(1, 0, color.NRGBA{255, 80, 40, 128})
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	raw := b.Bytes()
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	if err := VerifyExposurePreviewPNG(context.Background(), raw, digest, 2, 1, "", "affected"); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"protected", "before", "unknown"} {
		if VerifyExposurePreviewPNG(context.Background(), raw, digest, 2, 1, "", role) == nil {
			t.Fatal("invalid role/profile accepted", role)
		}
	}
	changed := append([]byte(nil), raw...)
	changed[len(changed)-1] ^= 1
	sum = sha256.Sum256(changed)
	if VerifyExposurePreviewPNG(context.Background(), changed, hex.EncodeToString(sum[:]), 2, 1, "", "affected") == nil {
		t.Fatal("invalid CRC accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if VerifyExposurePreviewPNG(ctx, raw, digest, 2, 1, "", "affected") != context.Canceled {
		t.Fatal("cancel ignored")
	}
}
