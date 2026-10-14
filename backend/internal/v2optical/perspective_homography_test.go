package v2optical

import (
	"context"
	"math"
	"testing"
)

func TestPerspectiveHomographyRejectsConditionAndHorizon(t *testing.T) {
	identity := [3][3]float64{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}
	if err := checkPerspectiveHomography(context.Background(), identity, 100, 80); err != nil {
		t.Fatal(err)
	}
	// Determinant remains above threshold; spectral condition alone must refuse.
	for _, m := range [][3][3]float64{
		{{1e9, 0, 0}, {0, 1, 0}, {0, 0, 1}},
		{{1, 0, 0}, {0, 1, 0}, {-.02, 0, 1}},
		{{1, 0, 0}, {0, 1, 0}, {0, 0, 1e-11}},
		{{1, 0, math.NaN()}, {0, 1, 0}, {0, 0, 1}},
	} {
		if err := checkPerspectiveHomography(context.Background(), m, 100, 80); err == nil {
			t.Fatal("invalid matrix accepted", m)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := checkPerspectiveHomography(ctx, identity, 100, 80); err != context.Canceled {
		t.Fatal(err)
	}
}
func TestPerspectiveAutoGeometryStrengthIdentityAndValidation(t *testing.T) {
	p := perspectiveIntent()
	p.Mode = "auto"
	p.Corners = nil
	p.Lines = &p.Base.Source
	p.MaxCropFraction = 1
	lines := PerspectiveLineSegments{SchemaVersion: 1, Lines: []PerspectiveLine{
		{A: PerspectivePoint{0, .2}, B: PerspectivePoint{1, .2}, Family: "horizontal"},
		{A: PerspectivePoint{0, .8}, B: PerspectivePoint{1, .8}, Family: "horizontal"},
		{A: PerspectivePoint{.2, 0}, B: PerspectivePoint{.2, 1}, Family: "vertical"},
		{A: PerspectivePoint{.8, 0}, B: PerspectivePoint{.8, 1}, Family: "vertical"}}}
	for _, strength := range []float64{0, .25, .5, 1} {
		p.Strength = strength
		got, err := CompilePerspectiveAutoGeometry(context.Background(), p, lines, 100, 80, 8000)
		if err != nil || got.Width != 100 || got.Height != 80 || got.CropFraction < 0 || got.CropFraction > 1 {
			t.Fatal(strength, got, err)
		}
	}
	p.Strength = 0
	if _, err := CompilePerspectiveAutoGeometry(context.Background(), p, lines, 100, 80, 7999); err == nil {
		t.Fatal("identity cap bypass")
	}
	lines.SchemaVersion = 0
	if _, err := CompilePerspectiveAutoGeometry(context.Background(), p, lines, 100, 80, 8000); err == nil {
		t.Fatal("identity document bypass")
	}
}
