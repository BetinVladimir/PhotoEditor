package v2optical

import (
	"bytes"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"testing"
)

func multiOriginals(t *testing.T) (PanoramaParams, [][]byte) {
	t.Helper()
	raw, err := os.ReadFile("testdata/negative-source.json")
	if err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(raw)
	if err != nil {
		t.Fatal(err)
	}
	p := PanoramaParams{SchemaVersion: 1, Frames: multiFrames(2), ReferenceOrdinal: 1, MaxPixels: 10000, Calibration: multiCalibration(), Projection: "planar"}
	// The baseline covers opaque frames; alpha-specific cases add their own plane.
	image.Alpha = nil
	originals := make([][]byte, 2)
	for i := range originals {
		image.Pixels.ID = uuid.New()
		originals[i], err = canonical.Marshal(image)
		if err != nil {
			t.Fatal(err)
		}
		p.Frames[i].Source.SHA256, err = canonical.Hash(image)
		if err != nil {
			t.Fatal(err)
		}
		p.Frames[i].CanvasHash, err = canonical.Hash(image.Canvas)
		if err != nil {
			t.Fatal(err)
		}
	}
	return p, originals
}
func TestMultiOriginalOrderAndDigest(t *testing.T) {
	p, originals := multiOriginals(t)
	raw, _, _ := p.Compile()
	images, err := VerifyMultiWorkingImages("optical.panorama", raw, originals)
	if err != nil || len(images) != 2 {
		t.Fatal(err)
	}
	if images[0].Pixels.ID == images[1].Pixels.ID {
		t.Fatal("order fixture collapsed")
	}
	if _, err = VerifyMultiWorkingImages("optical.panorama", raw, [][]byte{originals[1], originals[0]}); err == nil {
		t.Fatal("reorder accepted")
	}
	originals[0] = append(append([]byte{}, originals[0]...), 32)
	if _, err = VerifyMultiWorkingImages("optical.panorama", raw, originals); err == nil {
		t.Fatal("modified original accepted")
	}
}
func TestMultiPhysicalInputsKeepOccurrences(t *testing.T) {
	p, originals := multiOriginals(t)
	// Two versions may legitimately reference the same original document/pixels.
	originals[1] = append([]byte{}, originals[0]...)
	p.Frames[1].Source = p.Frames[0].Source
	raw, _, _ := p.Compile()
	entries, edges, err := MultiImageInputs("optical.panorama", raw, originals, multiCalibration())
	if err != nil || len(entries) != 1 || len(edges) != 2 {
		t.Fatalf("%v entries=%d edges=%d", err, len(entries), len(edges))
	}
	if edges[0].Ordinal != 0 || edges[1].Ordinal != 1 || edges[0].Binding.AssetVersionID == edges[1].Binding.AssetVersionID {
		t.Fatal("frame identity lost")
	}
	raw2, _, _ := p.Compile()
	if !bytes.Equal(raw, raw2) {
		t.Fatal("compiler mutated intent")
	}
}

func TestMultiOriginalsRefuseColorAndChildConflicts(t *testing.T) {
	for _, mode := range []string{"color", "child_digest", "child_root", "alpha_alias"} {
		t.Run(mode, func(t *testing.T) {
			p, originals := multiOriginals(t)
			first, err := review.ParseWorkingImage(originals[0])
			if err != nil {
				t.Fatal(err)
			}
			second, err := review.ParseWorkingImage(originals[1])
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "color":
				second.Color.ICCSHA256 = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
			case "child_digest":
				second.Pixels.ID = first.Pixels.ID
				second.Pixels.SHA256 = p.Calibration.SHA256
			case "child_root":
				second.Pixels.ID = p.Calibration.ArtifactID
			case "alpha_alias":
				alpha := second.Pixels
				second.Alpha = &alpha
			}
			originals[1], err = canonical.Marshal(second)
			if err != nil {
				t.Fatal(err)
			}
			p.Frames[1].Source.SHA256, err = canonical.Hash(second)
			if err != nil {
				t.Fatal(err)
			}
			raw, _, err := p.Compile()
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = MultiImageInputs("optical.panorama", raw, originals, multiCalibration()); err == nil {
				t.Fatal("conflicting source accepted")
			}
		})
	}
}
