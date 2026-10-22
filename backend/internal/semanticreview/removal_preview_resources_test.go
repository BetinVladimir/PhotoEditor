package semanticreview

import "testing"

func TestRemovalMemoryBackendPolicy(t *testing.T) {
	owner := ExposureOwnerCosts{RuntimeRAMBytes: 1, InputIndexScratchBytes: 16 * 1024 * 1024}
	limits := ExposureResourceEstimate{RAMBytes: 1 << 40, VRAMBytes: 1 << 40, ScratchBytes: 1 << 40, OutputBytes: 1 << 40, PixelCount: 200000000}
	for _, test := range []struct {
		backend string
		device  int64
		valid   bool
	}{
		{"cpu", 0, true}, {"cpu", 1, false}, {"cuda", 10000000000, true}, {"cuda", 10000000001, false}, {"mps", 12000000000, true}, {"mps", 0, false}, {"unknown", 1, false},
	} {
		cost, err := CompileRemovalPreviewResources(100, 100, owner, limits, test.backend, test.device)
		if (err == nil) != test.valid {
			t.Fatalf("%s %d: %v", test.backend, test.device, err)
		}
		if err == nil && cost.VRAMBytes != test.device {
			t.Fatal("measured device budget lost")
		}
	}
	cost, err := CompileRemovalPreviewResources(100, 100, owner, limits, "mps", 12000000000)
	if err != nil {
		t.Fatal(err)
	}
	limits.VRAMBytes = cost.VRAMBytes - 1
	if _, err := CompileRemovalPreviewResources(100, 100, owner, limits, "mps", 12000000000); err == nil {
		t.Fatal("insufficient device budget admitted")
	}
}

// Each owner limit is independent: a generous RAM allowance cannot conceal
// insufficient scratch, output quota, pixel capacity or measured device memory.
func TestRemovalPreviewResourcesRequireEveryExactBudget(t *testing.T) {
	owner := ExposureOwnerCosts{RuntimeRAMBytes: 4096, InputIndexScratchBytes: 16 * 1024 * 1024}
	generous := ExposureResourceEstimate{RAMBytes: 1 << 40, VRAMBytes: 1 << 40, ScratchBytes: 1 << 40, OutputBytes: 1 << 40, PixelCount: 200000000}
	cost, err := CompileRemovalPreviewResources(4096, 2048, owner, generous, "mps", 12000000000)
	if err != nil { t.Fatal(err) }
	if got, err := CompileRemovalPreviewResources(4096, 2048, owner, cost, "mps", 12000000000); err != nil || got != cost {
		t.Fatal("exact budgets must be sufficient", got, err)
	}
	for _, field := range []string{"ram", "device", "scratch", "output", "pixels"} {
		t.Run(field, func(t *testing.T) {
			limits := cost
			switch field {
			case "ram": limits.RAMBytes--
			case "device": limits.VRAMBytes--
			case "scratch": limits.ScratchBytes--
			case "output": limits.OutputBytes--
			case "pixels": limits.PixelCount--
			}
			got, err := CompileRemovalPreviewResources(4096, 2048, owner, limits, "mps", 12000000000)
			if err == nil || got != (ExposureResourceEstimate{}) { t.Fatal("partial budget admitted", got, err) }
		})
	}
}

func TestRemovalPreviewResourcesRejectUnsafeDimensionsAndOwnerCosts(t *testing.T) {
	const safe int64 = 9007199254740991
	limits := ExposureResourceEstimate{RAMBytes: safe, VRAMBytes: safe, ScratchBytes: safe, OutputBytes: safe, PixelCount: safe}
	owner := ExposureOwnerCosts{RuntimeRAMBytes: 1, InputIndexScratchBytes: 16 * 1024 * 1024}
	for _, size := range [][2]int64{{0,1},{1,0},{-1,1},{1,-1},{200001,1},{1,200001},{200000001,1},{200000000,2},{9223372036854775807,2}} {
		if cost, err := CompileRemovalPreviewResources(size[0],size[1],owner,limits,"cpu",0); err == nil || cost != (ExposureResourceEstimate{}) {
			t.Fatal("unsafe dimensions admitted", size, cost, err)
		}
	}
	for _, invalid := range []ExposureOwnerCosts{
		{RuntimeRAMBytes:0,InputIndexScratchBytes:16*1024*1024},
		{RuntimeRAMBytes:1,InputIndexScratchBytes:16*1024*1024-1},
		{RuntimeRAMBytes:safe,InputIndexScratchBytes:16*1024*1024},
		{RuntimeRAMBytes:1,InputIndexScratchBytes:safe},
	} {
		if cost, err := CompileRemovalPreviewResources(1,1,invalid,limits,"cpu",0); err == nil || cost != (ExposureResourceEstimate{}) {
			t.Fatal("unsafe owner costs admitted", invalid, cost, err)
		}
	}
}
