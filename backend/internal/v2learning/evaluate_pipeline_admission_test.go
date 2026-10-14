package v2learning

import (
	"fmt"
	"testing"
)

func TestHoldoutAdmissionRefusesUnimplementedEffectsBeforeTaskReservation(t *testing.T) {
	for _, name := range []string{"defects", "geometry", "culling", "series", "composition", "portrait_detail", "inpainting", "generation"} {
		t.Run(name, func(t *testing.T) {
			raw := fmt.Sprintf(`{"%s":{"enabled":true}}`, name)
			if name != "defects" && name != "geometry" {
				raw = fmt.Sprintf(`{"extensions":{"%s":{"enabled":true}}}`, name)
			}
			if validateFrozenHoldoutPipeline([]byte(raw)) == nil {
				t.Fatal("incomplete effect admitted")
			}
		})
	}
	for _, raw := range []string{
		`{"extensions":{"restoration":{"upscale":"2x","sensor_dust":"off"}}}`,
		`{"extensions":{"restoration":{"upscale":"off","sensor_dust":"review"}}}`,
		`{"extensions":{"background":{"mode":"generate"}}}`,
		`{"extensions":{"restoration":{}}}`,
	} {
		if validateFrozenHoldoutPipeline([]byte(raw)) == nil {
			t.Fatal("incomplete restoration admitted")
		}
	}
	for _, raw := range []string{`{}`, `{"beauty":{"enabled":true}}`,
		`{"extensions":{"restoration":{"upscale":"off","sensor_dust":"off"}}}`,
		`{"extensions":{"background":{"mode":"asset"}}}`,
	} {
		if err := validateFrozenHoldoutPipeline([]byte(raw)); err != nil {
			t.Fatalf("supported pipeline refused: %v", err)
		}
	}
}

func TestHoldoutAdmissionRequiresReviewedDustProof(t *testing.T) {
	raw := []byte(`{"extensions":{"restoration":{"upscale":"off","denoise":"off","sensor_dust":"review"}}}`)
	if validateFrozenHoldoutPipeline(raw) == nil {
		t.Fatal("dust admitted without evidence")
	}
	if err := validateFrozenHoldoutPipeline(raw, false, true); err != nil {
		t.Fatal("reviewed dust evidence refused", err)
	}
	if validateFrozenHoldoutPipeline(raw, true, true) == nil {
		t.Fatal("dust and defect evidence admitted together")
	}
}

func TestHoldoutAdmissionRequiresPortraitDetailEvidence(t *testing.T) {
	raw := []byte(`{"extensions":{"portrait_detail":{"enabled":true}}}`)
	if validateFrozenHoldoutPipeline(raw) == nil {
		t.Fatal("portrait detail admitted without evidence")
	}
	if err := validateFrozenHoldoutPipeline(raw, false, false, true); err != nil {
		t.Fatal("portrait detail evidence refused", err)
	}
	if validateFrozenHoldoutPipeline([]byte(`{}`), false, false, true) == nil {
		t.Fatal("detail evidence without effect admitted")
	}
	if validateFrozenHoldoutPipeline(raw, true, false, true) == nil ||
		validateFrozenHoldoutPipeline(raw, false, true, true) == nil {
		t.Fatal("conflicting reviewed effects admitted")
	}
}
