package masknpy

import (
	"bytes"
	"context"
	"math"
	"testing"
)

func TestCylindricalFieldCoordinatesAndCoverage(t *testing.T) {
	const width, height = 10, 8
	values := make([]float32, width*height*2)
	focal := float64(width)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			theta := (float64(x) + .5 - float64(width)/2) / focal
			sx := focal*math.Tan(theta) + float64(width)/2
			sy := (float64(y)+.5-float64(height)/2)/math.Cos(theta) + float64(height)/2
			if sx >= .5 && sx <= width-.5 && sy >= .5 && sy <= height-.5 {
				values[(y*width+x)*2] = float32(sx)
				values[(y*width+x)*2+1] = float32(sy)
			}
		}
	}
	verify := func(v []float32) error {
		raw := arrayFixture("(8,10,2)", v)
		return VerifyCylindricalFieldNPY(context.Background(), bytes.NewReader(raw), int64(len(raw)), arraySHAForBinary(raw), width, height)
	}
	if err := verify(values); err != nil {
		t.Fatal(err)
	}
	for _, index := range []int{0, (4*width + 5) * 2, (4*width+5)*2 + 1} {
		changed := append([]float32(nil), values...)
		changed[index] += 1
		if err := verify(changed); err == nil {
			t.Fatalf("incorrect coordinate accepted at %d", index)
		}
	}
	zeros := make([]float32, len(values))
	if err := verify(zeros); err == nil {
		t.Fatal("finite all-zero field accepted")
	}
}

// At width=3 only the centre column lies inside horizontal source coverage;
// theta=0 there, so these exact coordinates need no trigonometric fixture code.
func TestCylindricalExactCentreColumnGolden(t *testing.T) {
	values := []float32{0, 0, 1.5, .5, 0, 0, 0, 0, 1.5, 1.5, 0, 0, 0, 0, 1.5, 2.5, 0, 0}
	raw := arrayFixture("(3,3,2)", values)
	if err := VerifyCylindricalFieldNPY(context.Background(), bytes.NewReader(raw), int64(len(raw)), arraySHAForBinary(raw), 3, 3); err != nil {
		t.Fatal(err)
	}
}
