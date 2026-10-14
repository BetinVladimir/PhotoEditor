package v2input

import (
	"encoding/json"
	"testing"
)

func TestSemanticFaceParseCapacityBoundaries(t *testing.T) {
	costs := SemanticQualifiedCosts{RAMBytes: 256 << 20, VRAMBytes: 2 << 30, ScratchBytes: 64 << 20}
	body, err := CompileSemanticFaceParseProfile(6000, 4000, 300<<20, costs, 32<<30, 12<<30, 8<<30, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	var p sessionDescribeExecutionProfile
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatal(err)
	}
	e := p.ResourceEstimate
	output := int64(2*24000000 + 8192 + (1 << 20))
	if p.Backend != "cuda" || e.OutputBytes != output || e.PixelCount != 24000000 || e.RAMBytes != (1<<30)+128*24000000+output+(300<<20)+costs.RAMBytes || e.ScratchBytes != output+(300<<20)+costs.ScratchBytes || e.VRAMBytes != costs.VRAMBytes {
		t.Fatal("parser allocations omitted")
	}
	limits := [4]int64{e.RAMBytes, e.VRAMBytes, e.ScratchBytes, e.OutputBytes}
	if _, err := CompileSemanticFaceParseProfile(6000, 4000, 300<<20, costs, limits[0], limits[1], limits[2], limits[3]); err != nil {
		t.Fatal(err)
	}
	for i := range limits {
		bad := limits
		bad[i]--
		if body, err := CompileSemanticFaceParseProfile(6000, 4000, 300<<20, costs, bad[0], bad[1], bad[2], bad[3]); err == nil || body != nil {
			t.Fatal("underfunded parser accepted", i)
		}
	}
	costs.VRAMBytes = 0
	if _, err := CompileSemanticFaceParseProfile(6000, 4000, 300<<20, costs, 32<<30, 12<<30, 8<<30, 1<<30); err == nil {
		t.Fatal("missing qualified GPU peak accepted")
	}
}
