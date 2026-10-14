package masknpy

import (
	"bytes"
	"context"
	"errors"
	"math"
	"testing"
)

func TestCoverageSupportIntegrityAndEmptyEvidence(t *testing.T) {
	required := arrayFixture("(2,3)", []float32{1, .5, 0, 0, 0, 0})
	cover := arrayFixture("(2,3)", []float32{1e-20, .1, 1, 0, 0, 0})
	if n, e := VerifyMaskCoverage(context.Background(), pinnedMask(required), pinnedMask(cover), 3, 2); e != nil || n != 2 {
		t.Fatal(n, e)
	}
	empty := arrayFixture("(2,3)", make([]float32, 6))
	if n, e := VerifyMaskCoverage(context.Background(), pinnedMask(empty), pinnedMask(empty), 3, 2); e != nil || n != 0 {
		t.Fatal(n, e)
	}
	for _, bad := range [][]byte{
		arrayFixture("(2,3)", []float32{1, 0, 1, 0, 0, 0}),
		arrayFixture("(2,3)", []float32{1, 1, 0, 0, 0, float32(math.NaN())}),
		arrayFixture("(2,3)", []float32{1, 1, 0, 0, 0, 1.1}),
		arrayFixture("(3,2)", []float32{1, 1, 0, 0, 0, 0}),
		cover[:len(cover)-1], append(append([]byte(nil), cover...), 0),
	} {
		if n, e := VerifyMaskCoverage(context.Background(), pinnedMask(required), pinnedMask(bad), 3, 2); e == nil || n != 0 {
			t.Fatal("invalid coverage", n, e)
		}
	}
	changed := append([]byte(nil), cover...)
	changed[len(changed)-1] ^= 1
	pin := pinnedMask(cover)
	pin.Reader = bytes.NewReader(changed)
	if n, e := VerifyMaskCoverage(context.Background(), pinnedMask(required), pin, 3, 2); e == nil || n != 0 {
		t.Fatal("SHA substitution")
	}
	a, b := make([]float32, 20000), make([]float32, 20000)
	a[16384] = 1
	b[16384] = .01
	check := func() (int64, error) {
		return VerifyMaskCoverage(context.Background(), pinnedMask(arrayFixture("(1,20000)", a)), pinnedMask(arrayFixture("(1,20000)", b)), 20000, 1)
	}
	if n, e := check(); e != nil || n != 1 {
		t.Fatal(n, e)
	}
	b[16384] = 0
	if n, e := check(); e == nil || n != 0 {
		t.Fatal("boundary uncovered")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if n, e := VerifyMaskCoverage(ctx, pinnedMask(required), pinnedMask(cover), 3, 2); !errors.Is(e, context.Canceled) || n != 0 {
		t.Fatal(n, e)
	}
}

func TestCoverageMissingIsDistinctFromCorruptValues(t *testing.T) {
	required := arrayFixture("(2,3)", []float32{1, 0, 0, 0, 0, 0})
	missing := arrayFixture("(2,3)", make([]float32, 6))
	_, err := VerifyMaskCoverage(context.Background(), pinnedMask(required), pinnedMask(missing), 3, 2)
	if !errors.Is(err, ErrCoverageMissing) || !errors.Is(err, ErrInvalid) {
		t.Fatal("missing protection classification", err)
	}
	malformed := arrayFixture("(2,3)", []float32{float32(math.NaN()), 0, 0, 0, 0, 0})
	_, err = VerifyMaskCoverage(context.Background(), pinnedMask(required), pinnedMask(malformed), 3, 2)
	if !errors.Is(err, ErrInvalid) || errors.Is(err, ErrCoverageMissing) {
		t.Fatal("corrupt mask classified as missing coverage", err)
	}
}
