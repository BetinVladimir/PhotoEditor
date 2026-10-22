package review

import (
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

func TestRestorationReviewCandidateChangesOnlyRGBPixels(t *testing.T) {
	base, err := ParseWorkingImage(imageFixtures(t)[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"portrait.glare", "portrait.open_eyes", "image.reflection", "image.relight"} {
		for _, mode := range []string{"valid", "same_pixels", "same_content", "alpha", "canvas", "color", "dtype"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				candidate := base
				candidate.Pixels.ID = uuid.New()
				candidate.Pixels.SHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
				if candidate.Pixels.SHA256 == base.Pixels.SHA256 {
					candidate.Pixels.SHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
				}
				switch mode {
				case "same_content":
					candidate.Pixels.SHA256 = base.Pixels.SHA256
				case "same_pixels":
					candidate.Pixels = base.Pixels
				case "alpha":
					candidate.Alpha = &ArtifactRef{ID: uuid.New(), SHA256: candidate.Pixels.SHA256}
				case "canvas":
					candidate.Canvas.Width++
				case "color":
					candidate.Color.ICCSHA256 = candidate.Pixels.SHA256
				case "dtype":
					candidate.DType = "uint8"
				}
				raw, err := canonical.Marshal(candidate)
				if err != nil {
					t.Fatal(err)
				}
				baseRaw, err := canonical.Marshal(base)
				if err != nil {
					t.Fatal(err)
				}
				err = validateCandidatePair(kind, raw, baseRaw)
				valid := mode == "valid" || (mode == "same_content" && kind == "image.relight")
				if valid && err != nil {
					t.Fatal("restoration image refused", err)
				}
				if !valid && err == nil {
					t.Fatal("restoration metadata change accepted", mode)
				}
			})
		}
	}
}
