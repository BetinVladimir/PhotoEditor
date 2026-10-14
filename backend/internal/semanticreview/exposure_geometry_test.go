package semanticreview

import (
	"math"
	"testing"
)

func TestPlanarHomographyRejectsUnsafeSourceMappings(t *testing.T) {
	for _, matrix := range [][]float64{{1, 0, 0, 0, 1, 0, 0, 0, 1}, {2, 0, 10, 0, 2, -5, 0, 0, 1}, {-1, 0, 0, 0, -1, 0, 0, 0, -1}, {1, 0, 0, 0, 1, 0, .0001, .0001, 1}} {
		if !ValidatePlanarHomography(matrix, 256, 256) {
			t.Fatal("safe transform rejected", matrix)
		}
	}
	for _, matrix := range [][]float64{nil, {1, 0, 0, 0, 0, 0, 0, 0, 1}, {-1, 0, 0, 0, 1, 0, 0, 0, 1}, {1, 0, 0, 0, 1, 0, -.01, 0, 1}, {1, 0, 0, 0, 1, 0, 0, 0, 1e-13}, {1, 0, math.NaN(), 0, 1, 0, 0, 0, 1}, {1, 0, 0, 0, 1, 0, 0, math.Inf(1), 1}} {
		if ValidatePlanarHomography(matrix, 256, 256) {
			t.Fatal("unsafe transform accepted", matrix)
		}
	}
	identity := []float64{1, 0, 0, 0, 1, 0, 0, 0, 1}
	for _, size := range [][2]int64{{7, 256}, {256, 7}, {200000001, 8}, {20000, 20000}} {
		if ValidatePlanarHomography(identity, size[0], size[1]) {
			t.Fatal("invalid source dimensions")
		}
	}
}
