package masknpy

import (
	"context"
	"errors"
	"math"
	"testing"
)

func TestProbabilityCoveragePreservesMagnitude(t *testing.T) {
	required := arrayFixture("(2,3)", []float32{0, .25, .5, 1, 0, .8})
	equal := arrayFixture("(2,3)", []float32{1, .25, .5, 1, 0, .8})
	if n, err := VerifyProbabilityMaskCoverage(context.Background(), pinnedMask(required), pinnedMask(equal), 3, 2); err != nil || n != 4 {
		t.Fatal(n, err)
	}
	for _, values := range [][]float32{{0, .249, .5, 1, 0, .8}, {0, .25, .5, .001, 0, .8}, {0, .25, .5, 1, 0, float32(math.Nextafter32(.8, 0))}, {0, .25, .5, 1, float32(math.NaN()), .8}} {
		if n, err := VerifyProbabilityMaskCoverage(context.Background(), pinnedMask(required), pinnedMask(arrayFixture("(2,3)", values)), 3, 2); err == nil || n != 0 {
			t.Fatal("weakened protection accepted", n, err)
		}
	}
	weak := arrayFixture("(2,3)", []float32{0, .001, .001, .001, 0, .001})
	if n, err := VerifyMaskCoverage(context.Background(), pinnedMask(required), pinnedMask(weak), 3, 2); err != nil || n != 4 {
		t.Fatal("legacy boolean support changed", n, err)
	}
	empty := arrayFixture("(2,3)", make([]float32, 6))
	if n, err := VerifyProbabilityMaskCoverage(context.Background(), pinnedMask(empty), pinnedMask(equal), 3, 2); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	bad := pinnedMask(equal)
	bad.SHA256 = pinnedMask(empty).SHA256
	if n, err := VerifyProbabilityMaskCoverage(context.Background(), pinnedMask(required), bad, 3, 2); err == nil || n != 0 {
		t.Fatal("wrong protection SHA accepted")
	}
}

func TestMissingCoverageCannotHideStreamCorruption(t *testing.T) {
	required := arrayFixture("(2,3)", []float32{1, 0, 0, 0, 0, 0})
	weak := arrayFixture("(2,3)", []float32{0, 0, 0, 0, 0, 0})
	for _, magnitude := range []bool{false, true} {
		verify := VerifyMaskCoverage
		if magnitude {
			verify = VerifyProbabilityMaskCoverage
		}
		if n, err := verify(context.Background(), pinnedMask(required), pinnedMask(weak), 3, 2); n != 0 || !errors.Is(err, ErrCoverageMissing) {
			t.Fatal("valid missing support misclassified", n, err)
		}
		for _, kind := range []string{"sha", "required-sha", "late-nan", "trailing", "truncated"} {
			left, right := pinnedMask(required), pinnedMask(weak)
			switch kind {
			case "sha":
				right.SHA256 = left.SHA256
			case "required-sha":
				left.SHA256 = right.SHA256
			case "late-nan":
				right = pinnedMask(arrayFixture("(2,3)", []float32{0, 0, 0, 0, 0, float32(math.NaN())}))
			case "trailing":
				right = pinnedMask(append(append([]byte(nil), weak...), 0))
			case "truncated":
				right = pinnedMask(weak[:len(weak)-1])
			}
			if n, err := verify(context.Background(), left, right, 3, 2); n != 0 || err == nil || errors.Is(err, ErrCoverageMissing) {
				t.Fatalf("%s corrupt stream classified as valid missing coverage: %v", kind, err)
			}
		}
	}
}
