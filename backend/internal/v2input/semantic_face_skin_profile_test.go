package v2input

import (
	"encoding/json"
	"testing"
)

func TestSemanticSkinProfileCapacityBoundaries(t *testing.T) {
	const inputs int64 = 48 << 20
	body, err := CompileSemanticFaceSkinProfile(6000, 4000, inputs, 32<<30, 8<<30, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	var profile sessionDescribeExecutionProfile
	if err := json.Unmarshal(body, &profile); err != nil {
		t.Fatal(err)
	}
	e := profile.ResourceEstimate
	raw := int64(4000 * (6000*2 + 1))
	expected := int64(4*24000000+8192) + raw + raw/50 + 2*(1<<20)
	if profile.Backend != "cpu" || e.VRAMBytes != 0 || e.PixelCount != 24000000 || e.OutputBytes != expected || e.ScratchBytes != inputs+expected || e.RAMBytes != inputs+(256<<20)+16*24000000 {
		t.Fatal("worker allocations omitted")
	}
	limits := [3]int64{e.RAMBytes, e.ScratchBytes, e.OutputBytes}
	if _, err := CompileSemanticFaceSkinProfile(6000, 4000, inputs, limits[0], limits[1], limits[2]); err != nil {
		t.Fatal(err)
	}
	for i := range limits {
		bad := limits
		bad[i]--
		if body, err := CompileSemanticFaceSkinProfile(6000, 4000, inputs, bad[0], bad[1], bad[2]); err == nil || body != nil {
			t.Fatal("underfunded task admitted", i)
		}
	}
	for _, dimensions := range [][2]int64{{0, 4000}, {6000, 0}, {200000001, 1}, {200000000, 2}, {-1, 8}} {
		if _, err := CompileSemanticFaceSkinProfile(dimensions[0], dimensions[1], inputs, 32<<30, 8<<30, 1<<30); err == nil {
			t.Fatal("invalid dimensions accepted")
		}
	}
	if _, err := CompileSemanticFaceSkinProfile(8, 8, 9007199254740991, 9007199254740991, 9007199254740991, 9007199254740991); err == nil {
		t.Fatal("overflow accepted")
	}
}
