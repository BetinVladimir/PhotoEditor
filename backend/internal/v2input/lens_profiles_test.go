package v2input

import (
	"encoding/json"
	"testing"
)

func TestLensProfileMatchesWorkerBudgetAndOriginalPeak(t *testing.T) {
	for _, strength := range []float64{0, 1} {
		raw, err := CompileLensProfile(6000, 4000, strength, 100, 32<<30, 32<<30, 32<<30)
		if err != nil {
			t.Fatal(err)
		}
		var profile sessionDescribeExecutionProfile
		if err = json.Unmarshal(raw, &profile); err != nil {
			t.Fatal(err)
		}
		e := profile.ResourceEstimate
		channels := int64(28)
		if strength == 0 {
			channels = 4
		}
		if e.RAMBytes != 60*24000000+(512<<20) || e.ScratchBytes != 60*24000000+(64<<20) ||
			e.OutputBytes != channels*24000000+(32<<20) || e.PixelCount != 24000000 || e.VRAMBytes != 0 {
			t.Fatal(e)
		}
		for _, limits := range [][3]int64{{e.RAMBytes - 1, e.ScratchBytes, e.OutputBytes}, {e.RAMBytes, e.ScratchBytes - 1, e.OutputBytes}, {e.RAMBytes, e.ScratchBytes, e.OutputBytes - 1}} {
			if _, err = CompileLensProfile(6000, 4000, strength, 100, limits[0], limits[1], limits[2]); err == nil {
				t.Fatal("underbudget accepted")
			}
		}
	}
	raw, err := CompileLensProfile(2, 2, 1, 1<<30, 32<<30, 32<<30, 32<<30)
	if err != nil {
		t.Fatal(err)
	}
	var profile sessionDescribeExecutionProfile
	_ = json.Unmarshal(raw, &profile)
	if profile.ResourceEstimate.ScratchBytes != (1<<30)+(64<<20) {
		t.Fatal("original peak missing")
	}
}
