package v2input

import (
	"encoding/json"
	"testing"
)

func TestSemanticProfileMatchesPinnedLoaderCosts(t *testing.T) {
	for _, gamma := range [][2]bool{{false, false}, {true, false}, {false, true}, {true, true}} {
		qualification := SemanticQualifiedCosts{1000, 2000, 3000}
		raw, err := CompileSemanticTransferProfile(24, 20, 32, 16, gamma[0], gamma[1], qualification, 1<<40, 1<<40, 1<<40, 1<<40)
		if err != nil {
			t.Fatal(err)
		}
		var p sessionDescribeExecutionProfile
		if json.Unmarshal(raw, &p) != nil {
			t.Fatal("profile JSON")
		}
		conversion := int64(0)
		if gamma[0] {
			conversion += 12*480 + 8192
		}
		if gamma[1] {
			conversion += 12*512 + 8192
		}
		expected := sessionDescribeEstimate{RAMBytes: 20*(480+512) + (320 << 20) + conversion + 1000, VRAMBytes: 2000,
			ScratchBytes: 4*512 + 8192 + (8 << 20) + 12*(480+512) + 8*480 + 4*512 + 5*8192 + conversion + 3000,
			OutputBytes:  4*512 + 4096 + (24 << 20), PixelCount: 992}
		if p.Backend != "cuda" || p.RendererVersion != "v2.2" || p.ResourceEstimate != expected {
			t.Fatalf("wrong costs: %+v", p)
		}
		for n := 0; n < 4; n++ {
			limits := []int64{expected.RAMBytes, expected.VRAMBytes, expected.ScratchBytes, expected.OutputBytes}
			limits[n]--
			if _, err := CompileSemanticTransferProfile(24, 20, 32, 16, gamma[0], gamma[1], qualification, limits[0], limits[1], limits[2], limits[3]); err == nil {
				t.Fatal("short admission accepted")
			}
		}
	}
}
func TestSemanticProfileMaximumCanvasAndOverflow(t *testing.T) {
	raw, err := CompileSemanticTransferProfile(20000, 10000, 20000, 10000, false, false, SemanticQualifiedCosts{}, 1<<40, 1<<40, 1<<40, 1<<40)
	if err != nil {
		t.Fatal(err)
	}
	var p sessionDescribeExecutionProfile
	json.Unmarshal(raw, &p)
	if p.ResourceEstimate.OutputBytes != 825169920 {
		t.Fatal("operation cap")
	}
	if _, err := CompileSemanticTransferProfile(20000, 10001, 8, 8, false, false, SemanticQualifiedCosts{}, 1<<40, 1<<40, 1<<40, 1<<40); err == nil {
		t.Fatal("oversized source")
	}
	if _, err := CompileSemanticTransferProfile(8, 8, 8, 8, false, false, SemanticQualifiedCosts{RAMBytes: 9007199254740991}, 9007199254740991, 0, 1<<40, 1<<40); err == nil {
		t.Fatal("RAM overflow")
	}
}
