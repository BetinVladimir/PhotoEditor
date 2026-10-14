package v2optical

import (
	"context"
	"math"
	"testing"
)

func TestPerspectiveManualGeometryMapsQuad(t *testing.T) {
	p := perspectiveIntent()
	p.Corners = []PerspectivePoint{{.2, 0}, {.8, 0}, {.8, 1}, {.2, 1}}
	p.Strength = 1
	p.MaxCropFraction = .5
	got, err := CompilePerspectiveManualGeometry(context.Background(), p, 100, 80, 8000)
	if err != nil || got.Width != 60 || got.Height != 80 {
		t.Fatal(got, err)
	}
	targets := [4][2]float64{{0, 0}, {60, 0}, {60, 80}, {0, 80}}
	for i, point := range got.Corners {
		m := got.SourceToOutput
		den := m[2][0]*point.X + m[2][1]*point.Y + m[2][2]
		for axis := 0; axis < 2; axis++ {
			value := (m[axis][0]*point.X + m[axis][1]*point.Y + m[axis][2]) / den
			if math.Abs(value-targets[i][axis]) > 1e-9 {
				t.Fatal(i, axis, value)
			}
		}
	}
	p.Strength = 0
	got, err = CompilePerspectiveManualGeometry(context.Background(), p, 100, 80, 8000)
	if err != nil || got.SourceToOutput != ([3][3]float64{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}) {
		t.Fatal(got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CompilePerspectiveManualGeometry(ctx, p, 100, 80, 8000); err != context.Canceled {
		t.Fatal(err)
	}
}
func TestPerspectiveManualGeometryRejectsUnstableQuad(t *testing.T) {
	p := perspectiveIntent()
	p.Strength = 1
	p.MaxCropFraction = 1
	p.Corners = []PerspectivePoint{{.5, 0}, {.500000000001, 0}, {.500000000001, 1}, {.5, 1}}
	if _, err := CompilePerspectiveManualGeometry(context.Background(), p, 100, 80, 8000); err == nil {
		t.Fatal("ill-conditioned geometry accepted")
	}
}
