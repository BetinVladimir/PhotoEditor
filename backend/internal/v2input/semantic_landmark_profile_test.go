package v2input

import (
	"encoding/json"
	"testing"
)

func TestSemanticLandmarkProfileCapacityBoundaries(t *testing.T) {
	costs := SemanticQualifiedCosts{RAMBytes: 256 << 20, VRAMBytes: 2 << 30, ScratchBytes: 64 << 20}
	body, err := CompileSemanticLandmarkProfile(6000, 4000, 300<<20, costs, 32<<30, 12<<30, 8<<30, 8<<20)
	if err != nil {
		t.Fatal(err)
	}
	var profile sessionDescribeExecutionProfile
	if err := json.Unmarshal(body, &profile); err != nil {
		t.Fatal(err)
	}
	e := profile.ResourceEstimate
	if profile.Backend != "cuda" || e.PixelCount != 24000000 || e.OutputBytes != 8<<20 || e.RAMBytes != (1<<30)+128*24000000+(8<<20)+(300<<20)+costs.RAMBytes || e.VRAMBytes != costs.VRAMBytes || e.ScratchBytes != (8<<20)+(300<<20)+costs.ScratchBytes {
		t.Fatal("FAN2D allocations missing")
	}
	limits := [4]int64{e.RAMBytes, e.VRAMBytes, e.ScratchBytes, e.OutputBytes}
	if _, err := CompileSemanticLandmarkProfile(6000, 4000, 300<<20, costs, limits[0], limits[1], limits[2], limits[3]); err != nil {
		t.Fatal(err)
	}
	for i := range limits {
		bad := limits
		bad[i]--
		if body, err := CompileSemanticLandmarkProfile(6000, 4000, 300<<20, costs, bad[0], bad[1], bad[2], bad[3]); err == nil || body != nil {
			t.Fatal("underfunded FAN2D task accepted", i)
		}
	}
	costs.VRAMBytes = 0
	if _, err := CompileSemanticLandmarkProfile(6000, 4000, 300<<20, costs, 32<<30, 12<<30, 8<<30, 8<<20); err == nil {
		t.Fatal("unqualified GPU costs accepted")
	}
}
