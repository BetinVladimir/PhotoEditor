package review

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestRenderReviewCandidatePairBindsSourceGeometry(t *testing.T) {
	baseRaw := imageFixtures(t)[0]
	base, err := ParseWorkingImage(baseRaw)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"valid", "source", "canvas", "invalid_candidate", "invalid_base"} {
		t.Run(mode, func(t *testing.T) {
			candidate := base
			candidate.Pixels.ID = uuid.New()
			if mode == "source" {
				candidate.Canvas.SourceSHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
				if candidate.Canvas.SourceSHA256 == base.Canvas.SourceSHA256 {
					candidate.Canvas.SourceSHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
				}
			}
			if mode == "canvas" {
				candidate.Canvas.Width++
			}
			raw, err := canonical.Marshal(candidate)
			if err != nil {
				t.Fatal(err)
			}
			before := baseRaw
			if mode == "invalid_candidate" {
				raw = []byte(`{}`)
			}
			if mode == "invalid_base" {
				before = []byte(`{}`)
			}
			err = validateCandidatePair("render.compose", raw, before)
			if mode == "valid" {
				if err != nil {
					t.Fatal("same-canvas pair refused", err)
				}
				return
			}
			if err == nil {
				t.Fatal("incompatible candidate pair accepted")
			}
		})
	}
}
