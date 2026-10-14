package v2optical

import (
	"math"
	"testing"
)

func TestPerspectiveManualStrengthBounds(t *testing.T) {
	p := perspectiveIntent()
	p.Corners = []PerspectivePoint{{.2, 0}, {.8, 0}, {.8, 1}, {.2, 1}}
	for _, test := range []struct {
		strength float64
		width    int64
		crop     float64
	}{{0, 100, 0}, {.5, 80, .2}, {1, 60, .4}} {
		p.Strength = test.strength
		bounds, err := CompilePerspectiveManualBounds(p, 100, 80, 8000)
		if err != nil || bounds.Width != test.width || bounds.Height != 80 || math.Abs(bounds.CropFraction-test.crop) > 1e-12 {
			t.Fatal(bounds, err)
		}
		resources, err := PerspectiveResources(100, 80, bounds.Width, bounds.Height)
		if err != nil || resources.PixelCount != test.width*80 {
			t.Fatal(resources, err)
		}
	}
}
func TestPerspectiveManualTiesEvenAndLimits(t *testing.T) {
	p := perspectiveIntent()
	p.Corners = []PerspectivePoint{{.25, 0}, {.75, 0}, {.75, 1}, {.25, 1}}
	bounds, err := CompilePerspectiveManualBounds(p, 5, 4, 20)
	if err != nil || bounds.Width != 2 || bounds.Height != 4 {
		t.Fatal(bounds, err)
	}
	if _, err := CompilePerspectiveManualBounds(p, 5, 4, 7); err == nil {
		t.Fatal("output cap ignored")
	}
	p.MaxCropFraction = .49
	if _, err := CompilePerspectiveManualBounds(p, 5, 4, 20); err == nil {
		t.Fatal("crop cap ignored")
	}
	p.MaxCropFraction = .5
	p.Mode = "auto"
	p.Corners = nil
	if _, err := CompilePerspectiveManualBounds(p, 5, 4, 20); err == nil {
		t.Fatal("manual solver accepted auto")
	}
}

func TestPerspectiveIdentityBoundsRetainSourceCanvas(t *testing.T) {
	p := perspectiveIntent()
	p.Mode = "auto"
	p.Corners = nil
	p.Lines = &p.Base.Source
	p.Strength = 0
	bounds, err := CompilePerspectiveBounds(p, 101, 79, 7979)
	if err != nil || bounds.Width != 101 || bounds.Height != 79 || bounds.CropFraction != 0 || bounds.Corners[2] != (PerspectivePoint{101, 79}) {
		t.Fatal(bounds, err)
	}
	for _, size := range [][3]int64{{101, 79, 7978}, {0, 79, 7979}, {101, 0, 7979}, {101, 79, 200000001}} {
		if _, err := CompilePerspectiveBounds(p, size[0], size[1], size[2]); err == nil {
			t.Fatal("identity bypassed capacity", size)
		}
	}
	p.Lines = nil
	if _, err := CompilePerspectiveBounds(p, 101, 79, 7979); err == nil {
		t.Fatal("identity bypassed Params validation")
	}
	p.Lines = &p.Base.Source
	p.Strength = .1
	if _, err := CompilePerspectiveBounds(p, 101, 79, 7979); err == nil {
		t.Fatal("unsupported auto solver admitted")
	}
}
