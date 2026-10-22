package review

import (
 "encoding/json"
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

// Local portrait may replace RGB only, including a zero-strength exact bypass.
func TestLocalPortraitReviewPreservesWholeImageDescriptor(t *testing.T) {
 baseRaw := imageFixtures(t)[0]
 var base map[string]interface{}
 if err := json.Unmarshal(baseRaw, &base); err != nil { t.Fatal(err) }
 for _, kind := range []string{"portrait.hair", "portrait.clothing"} {
  t.Run(kind, func(t *testing.T) {
   if err := validateCandidatePair(kind, baseRaw, baseRaw); err != nil { t.Fatal("exact bypass refused", err) }
   parsed, err := ParseWorkingImage(baseRaw); if err != nil { t.Fatal(err) }
   parsed.Pixels.ID = uuid.New()
   rgbOnly, err := canonical.Marshal(parsed); if err != nil { t.Fatal(err) }
   if err := validateCandidatePair(kind, rgbOnly, baseRaw); err != nil { t.Fatal("RGB-only result refused", err) }
   for _, field := range []string{"alpha", "color", "canvas", "transform_chain"} {
    t.Run(field, func(t *testing.T) {
     var changed map[string]interface{}
     if err := json.Unmarshal(baseRaw, &changed); err != nil { t.Fatal(err) }
     // Removing a descriptor is either a malformed document or a forbidden change.
     delete(changed, field)
     raw, err := json.Marshal(changed); if err != nil { t.Fatal(err) }
     if string(raw) == "" { t.Fatal("empty fixture") }
     if _, exists := base[field]; !exists { t.Skip("fixture does not contain optional field") }
     if err := validateCandidatePair(kind, raw, baseRaw); err == nil { t.Fatal("descriptor change accepted", field) }
    })
   }
  })
 }
}
