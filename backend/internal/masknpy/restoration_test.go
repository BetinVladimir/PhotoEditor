package masknpy

import (
	"context"
	"math"
	"testing"
)

func TestRestorationPixelDeltaExactOutsideAndMaskLineage(t *testing.T) {
	source := []float32{.1, .2, .3, .4, .5, .6, .7, .8, .9, .11, .22, .33}
	output := append([]float32(nil), source...)
	output[3] = .45
	target := []float32{0, 1, 0, 0}
	guard := []float32{0, 0, 0, 0}
	changed := []float32{0, 1, 0, 0}
	generated := []float32{0, 1, 0, 0}
	verify := func(src, out []float32, target, guard, changed, generated []float32) (int64, error) {
		a := arrayFixture("(2,2,3)", src)
		b := arrayFixture("(2,2,3)", out)
		c := arrayFixture("(2,2)", target)
		d := arrayFixture("(2,2)", guard)
		e := arrayFixture("(2,2)", changed)
		var g *PinnedMask
		if generated != nil {
			raw := arrayFixture("(2,2)", generated)
			pin := pinnedMask(raw)
			g = &pin
		}
		return VerifyRestorationPixelDelta(context.Background(), pinnedMask(a), pinnedMask(b),
			pinnedMask(c), pinnedMask(d), pinnedMask(e), g, 2, 2)
	}
	if count, err := verify(source, output, target, guard, changed, generated); err != nil || count != 1 {
		t.Fatal("exact proposal refused", count, err)
	}
	for name, mutate := range map[string]func(){
		"outside":           func() { target[1] = 0 },
		"protected":         func() { guard[1] = 1 },
		"changed lie":       func() { changed[1] = 0 },
		"generated outside": func() { generated[0] = 1 },
		"nonfinite":         func() { output[3] = float32(math.NaN()) },
	} {
		originalTarget, originalGuard, originalChanged, originalGenerated, originalOutput :=
			append([]float32(nil), target...), append([]float32(nil), guard...),
			append([]float32(nil), changed...), append([]float32(nil), generated...),
			append([]float32(nil), output...)
		mutate()
		if count, err := verify(source, output, target, guard, changed, generated); err == nil || count != 0 {
			t.Fatal("invalid restoration pixels accepted", name, count, err)
		}
		target, guard, changed, generated, output =
			originalTarget, originalGuard, originalChanged, originalGenerated, originalOutput
	}
	if count, err := verify(source, output, target, guard, changed, nil); err != nil || count != 1 {
		t.Fatal("donor mode without generation refused", count, err)
	}
}
