package v2optical

import (
	"context"
	"math"
	"testing"
)

func TestPerspectiveVanishingParallelAndFinite(t *testing.T) {
	cases := []struct {
		lines []PerspectiveLine
		point [3]float64
	}{
		{[]PerspectiveLine{{A: PerspectivePoint{0, .2}, B: PerspectivePoint{1, .2}}, {A: PerspectivePoint{0, .8}, B: PerspectivePoint{1, .8}}}, [3]float64{1, 0, 0}},
		{[]PerspectiveLine{{A: PerspectivePoint{0, 0}, B: PerspectivePoint{1, .5}}, {A: PerspectivePoint{0, 1}, B: PerspectivePoint{1, .5}}}, [3]float64{100, 40, 1}},
	}
	for _, c := range cases {
		got, err := perspectiveVanishingPoint(context.Background(), c.lines, 100, 80)
		if err != nil {
			t.Fatal(err)
		}
		norm := math.Sqrt(c.point[0]*c.point[0] + c.point[1]*c.point[1] + c.point[2]*c.point[2])
		dot := 0.
		for i := 0; i < 3; i++ {
			dot += got[i] * c.point[i] / norm
		}
		if math.Abs(math.Abs(dot)-1) > 1e-10 {
			t.Fatal(got, c.point)
		}
	}
}
func TestPerspectiveVanishingRejectsDegenerateAndCancellation(t *testing.T) {
	line := PerspectiveLine{A: PerspectivePoint{0, .2}, B: PerspectivePoint{1, .2}}
	for _, lines := range [][]PerspectiveLine{nil, {line}, {line, line}, {{A: PerspectivePoint{0, 0}, B: PerspectivePoint{0, 0}}, line}, {{A: PerspectivePoint{math.NaN(), 0}, B: PerspectivePoint{1, 0}}, line}} {
		if _, err := perspectiveVanishingPoint(context.Background(), lines, 100, 80); err == nil {
			t.Fatal("invalid family accepted", lines)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := perspectiveVanishingPoint(ctx, []PerspectiveLine{line, line}, 100, 80); err != context.Canceled {
		t.Fatal(err)
	}
}

func TestPerspectiveVanishingGeometryAxisAligned(t *testing.T) {
	lines := PerspectiveLineSegments{SchemaVersion: 1, Lines: []PerspectiveLine{
		{A: PerspectivePoint{0, .2}, B: PerspectivePoint{1, .2}, Family: "horizontal"},
		{A: PerspectivePoint{0, .8}, B: PerspectivePoint{1, .8}, Family: "horizontal"},
		{A: PerspectivePoint{.2, 0}, B: PerspectivePoint{.2, 1}, Family: "vertical"},
		{A: PerspectivePoint{.8, 0}, B: PerspectivePoint{.8, 1}, Family: "vertical"},
	}}
	got, err := CompilePerspectiveVanishingGeometry(context.Background(), lines, 100, 80)
	if err != nil || math.Abs(got.Horizon[0]) > 1e-12 || math.Abs(got.Horizon[1]) > 1e-12 || got.Horizon[2] != 1 {
		t.Fatal(got, err)
	}
	lines.Lines[3].Family = "unknown"
	if _, err := CompilePerspectiveVanishingGeometry(context.Background(), lines, 100, 80); err == nil {
		t.Fatal("unknown family accepted")
	}
}
