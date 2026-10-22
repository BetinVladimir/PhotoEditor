package semanticreview

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"image"
	"image/color"
	"image/png"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestPhysicalBackgroundCleanupPreviewRequiresAuthorityBeforeIO(t *testing.T) {
	got, err := ReadPhysicalBackgroundCleanupPreview(context.Background(), nil, nil, "", auth.Identity{}, uuid.New(), BackgroundCleanupPreviewParams{ExposurePreviewParams: previewParamsFixture(), Kind: "background_cleanup", ModelID: "big_lama", ContextPixels: 64, FeatherPixels: 8}, []byte("{}"), "", "")
	if err == nil || got.verified {
		t.Fatal("unverified preview escaped")
	}
}

// Exercises real PNG bytes through the shared physical reader used by cleanup.
// Store identity is recorded; this is not SQL authority or object-store proof.
func TestBackgroundCleanupPhysicalOverlayReadPinsClosesAndRejectsCorruption(t *testing.T) {
	for _, role := range []string{"affected", "protected"} {
		im := image.NewNRGBA(image.Rect(0, 0, 2, 1))
		c := color.NRGBA{255, 80, 40, 128}
		if role == "protected" {
			c = color.NRGBA{0, 220, 255, 128}
		}
		im.SetNRGBA(0, 0, c)
		im.SetNRGBA(1, 0, c)
		var b bytes.Buffer
		if err := png.Encode(&b, im); err != nil {
			t.Fatal(err)
		}
		raw := b.Bytes()
		sum := sha256.Sum256(raw)
		source := PlanSource{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}, Bucket: "fixture", Key: "overlay", Version: "immutable-v1", Bytes: int64(len(raw)), MediaType: "image/png"}
		for _, mode := range []string{"valid", "truncated", "oversized", "corrupt", "close_error"} {
			store := &planStoreFixture{Body: append([]byte(nil), raw...)}
			failure := errors.New("close failed")
			switch mode {
			case "truncated":
				store.Body = store.Body[:len(store.Body)-1]
			case "oversized":
				store.Body = append(store.Body, 0)
			case "corrupt":
				store.Body[len(store.Body)-1] ^= 1
			case "close_error":
				store.CloseErr = failure
			}
			err := readAndVerifyPreviewPNG(context.Background(), store, "fixture", source, 2, 1, "", role)
			if (mode == "valid") != (err == nil) {
				t.Fatal(role, mode, err)
			}
			if mode == "close_error" && err != failure {
				t.Fatal("close failure hidden", err)
			}
			if store.Closed != 1 || len(store.Opened) != 1 {
				t.Fatal("stream lifecycle", mode, store.Closed)
			}
			opened := store.Opened[0]
			if opened.Key != source.Key || opened.VersionID != source.Version || opened.SHA256 != source.Ref.SHA256 || opened.Bytes != source.Bytes || opened.ContentType != source.MediaType {
				t.Fatal("object identity changed", opened)
			}
		}
	}
}
