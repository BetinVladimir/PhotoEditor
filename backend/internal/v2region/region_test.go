package v2region

import (
	"math"
	"testing"
)

func TestRegionGeometryBoundsAndOrientedCoordinates(t *testing.T) {
	cases := []struct {
		w, h int64
		r    RegionRect
		edge int
		want RegionGeometry
	}{
		{6000, 4000, RegionRect{.25, .25, .75, .75}, 1024, RegionGeometry{1500, 1000, 3000, 2000, 1024, 682}},
		{4000, 6000, RegionRect{.25, .25, .75, .75}, 1024, RegionGeometry{1000, 1500, 2000, 3000, 682, 1024}},
		{100, 50, RegionRect{0, 0, 1, 1}, 1024, RegionGeometry{0, 0, 100, 50, 100, 50}},
		{100, 50, RegionRect{.501, .501, .502, .502}, 64, RegionGeometry{50, 25, 1, 1, 1, 1}}}
	for _, c := range cases {
		got, err := CompileRegionGeometry(c.w, c.h, c.r, c.edge)
		if err != nil || got != c.want {
			t.Fatalf("got %+v %v; want %+v", got, err, c.want)
		}
	}
}
func TestRegionGeometryRejectsInvalidRequests(t *testing.T) {
	for _, r := range []RegionRect{{0, 0, 0, 1}, {0, 1, 1, 0}, {-1, 0, 1, 1}, {0, 0, 2, 1}, {math.NaN(), 0, 1, 1}, {0, 0, math.Inf(1), 1}} {
		if _, err := CompileRegionGeometry(100, 100, r, 1024); err != ErrRegionInvalid {
			t.Fatal("invalid rectangle accepted")
		}
	}
	for _, c := range [][3]int64{{0, 1, 64}, {20001, 10000, 64}, {1, 1, 63}, {1, 1, 1025}} {
		if _, err := CompileRegionGeometry(c[0], c[1], RegionRect{0, 0, 1, 1}, int(c[2])); err != ErrRegionInvalid {
			t.Fatal("invalid dimensions admitted")
		}
	}
}
