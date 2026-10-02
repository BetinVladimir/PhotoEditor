package v2region

import (
	"encoding/json"
	"testing"
)

func TestRegionSourceResourceBudget(t *testing.T) {
	shape := RegionSourceShape{Width: 400, Height: 300}
	opaque, err := shape.ResourceBudget()
	if err != nil {
		t.Fatal(err)
	}
	if opaque.PixelCount != 120000 || opaque.ScratchBytes != 1444096 || opaque.RAMBytes != 21581312 {
		t.Fatal("opaque source budget", opaque)
	}
	shape.HasAlpha = true
	alpha, err := shape.ResourceBudget()
	if err != nil {
		t.Fatal(err)
	}
	if alpha.ScratchBytes-opaque.ScratchBytes != 2*(480000+4096) || alpha.RAMBytes-opaque.RAMBytes != 2*(480000+4096) {
		t.Fatal("alpha normalization budget", opaque, alpha)
	}
	for _, shape := range []RegionSourceShape{{Width: 0, Height: 1}, {Width: 200000001, Height: 1}, {Width: 20000, Height: 20000}} {
		if _, err := shape.ResourceBudget(); err == nil {
			t.Fatal("invalid canvas accepted")
		}
	}
}
func TestRegionProfileSourceBudget(t *testing.T) {
	shape := RegionSourceShape{Width: 400, Height: 300, HasAlpha: true}
	budget, _ := shape.ResourceBudget()
	estimate := map[string]any{"pixel_count": budget.PixelCount, "ram_bytes": budget.RAMBytes, "scratch_bytes": budget.ScratchBytes}
	profile := func() []byte { raw, _ := json.Marshal(map[string]any{"resource_estimate": estimate}); return raw }
	if err := validateRegionSourceBudget(profile(), shape); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"pixel_count", "ram_bytes", "scratch_bytes"} {
		original := estimate[key]
		estimate[key] = original.(int64) - 1
		if err := validateRegionSourceBudget(profile(), shape); err == nil {
			t.Fatal("underbudget accepted", key)
		}
		estimate[key] = true
		if err := validateRegionSourceBudget(profile(), shape); err == nil {
			t.Fatal("boolean accepted", key)
		}
		estimate[key] = original
	}
	if err := validateRegionSourceBudget([]byte(`{}`), shape); err == nil {
		t.Fatal("missing estimate accepted")
	}
}

func TestRegionProfileRespectsExistingStyleParserBudget(t *testing.T) {
	profile := func(ram, output any) []byte {
		raw, _ := json.Marshal(map[string]any{"resource_estimate": map[string]any{"ram_bytes": ram, "output_bytes": output}})
		return raw
	}
	if err := validateRegionArtifactBudget(profile(int64(3)<<30, 1024)); err != nil {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{profile((int64(3)<<30)-1, 1024), profile(int64(3)<<30, 0), profile(int64(3)<<30, true), profile(true, 1024), profile(int64(3)<<30, int64(9007199254740992)), []byte(`{}`)} {
		if err := validateRegionArtifactBudget(raw); err == nil {
			t.Fatal("unusable artifact profile accepted", string(raw))
		}
	}
}
