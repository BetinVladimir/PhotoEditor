package masknpy

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"testing"
)

func TestProbabilityPNGOriginalPythonWire(t *testing.T) {
	body, err := os.ReadFile("testdata/probability-png-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Width, Height int64
		NPY, PNG      []byte
	}
	if err := json.Unmarshal(body, &fixture); err != nil {
		t.Fatal(err)
	}
	if err := VerifyProbabilityMaskPNG(context.Background(), labelPin(fixture.NPY), labelPin(fixture.PNG), fixture.Width, fixture.Height); err != nil {
		t.Fatal(err)
	}
	if err := VerifyDefectLabelPNG(context.Background(), labelPin(fixture.NPY), labelPin(fixture.PNG), fixture.Width, fixture.Height); err == nil {
		t.Fatal("float32 accepted as labels")
	}
}
func TestProbabilityPNGQuantizationAndRange(t *testing.T) {
	values := []float32{0, .5, 1, .1, .25, .75}
	png := testLabelPNG([]uint16{0, 32768, 65535, 6554, 16384, 49151}, 3, 2, 0)
	if err := VerifyProbabilityMaskPNG(context.Background(), labelPin(arrayFixture("(2,3)", values)), labelPin(png), 3, 2); err != nil {
		t.Fatal(err)
	}
	for _, value := range []float32{-.001, 1.001, float32(math.NaN()), float32(math.Inf(1))} {
		bad := append([]float32(nil), values...)
		bad[0] = value
		if err := VerifyProbabilityMaskPNG(context.Background(), labelPin(arrayFixture("(2,3)", bad)), labelPin(png), 3, 2); err == nil {
			t.Fatal("invalid probability accepted", value)
		}
	}
	wrong := testLabelPNG([]uint16{0, 32767, 65535, 6554, 16384, 49151}, 3, 2, 0)
	if err := VerifyProbabilityMaskPNG(context.Background(), labelPin(arrayFixture("(2,3)", values)), labelPin(wrong), 3, 2); err == nil {
		t.Fatal("incorrect half rounding accepted")
	}
}
