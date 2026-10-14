package v2input

import (
	"encoding/json"
	"testing"
)

func TestCapturePrepareProfileCoversFloat32Alpha(t *testing.T) {
	const pixels = int64(2048 * 2048)
	for _, alpha := range []bool{false, true} {
		channels := int64(12)
		if alpha {
			channels = 16
		}
		raw, err := CompileCapturePrepareProfile("cpu", 2048, 2048, alpha, 32<<30, 0, 32<<30)
		if err != nil {
			t.Fatal(err)
		}
		var profile sessionDescribeExecutionProfile
		if err = json.Unmarshal(raw, &profile); err != nil {
			t.Fatal(err)
		}
		estimate := profile.ResourceEstimate
		if estimate.OutputBytes != channels*pixels+(16<<20) || estimate.ScratchBytes != channels*2*pixels+(16<<20) {
			t.Fatal(estimate)
		}
		if _, err = CompileCapturePrepareProfile("cpu", 2048, 2048, alpha, 32<<30, 0, estimate.ScratchBytes-1); err == nil {
			t.Fatal("underbudget accepted")
		}
	}
}

func TestCapturePrepareProfileMPSUsesQualifiedGPUBudget(t *testing.T) {
	raw, err := CompileCapturePrepareProfile("mps", 2048, 2048, true,
		32<<30, 10_000_000_000, 32<<30)
	if err != nil {
		t.Fatal(err)
	}
	var profile sessionDescribeExecutionProfile
	if err = json.Unmarshal(raw, &profile); err != nil || profile.Backend != "mps" ||
		profile.ResourceEstimate.VRAMBytes != 6<<30 {
		t.Fatal("MPS capture reservation invalid", profile, err)
	}
	if _, err = CompileCapturePrepareProfile("mps", 2048, 2048, true,
		32<<30, (6<<30)-1, 32<<30); err == nil {
		t.Fatal("MPS capture accepted insufficient VRAM")
	}
}
