package semanticreview

import "testing"

func TestPresentationDimensionsNoUpscaleAndNearestEven(t *testing.T) {
	for _, s := range [][4]int64{{1, 1, 1, 1}, {13, 7, 13, 7}, {6000, 4000, 2560, 1707}, {4000, 6000, 1707, 2560}, {200000, 1, 2560, 1}, {5120, 5, 2560, 2}, {5120, 7, 2560, 4}, {0, 5, 0, 0}} {
		w, h := previewDisplayDimensions(s[0], s[1])
		if w != int(s[2]) || h != int(s[3]) {
			t.Fatal(s, w, h)
		}
	}
}
