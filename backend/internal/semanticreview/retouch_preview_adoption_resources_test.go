package semanticreview

import "testing"

func TestShownRetouchCopyResourcesChargeTwoNPYFilesWithoutGPU(t *testing.T) {
	owner := ExposureOwnerCosts{RuntimeRAMBytes: 12345, InputIndexScratchBytes: 16 * 1024 * 1024}
	limits := ExposureResourceEstimate{RAMBytes: 9007199254740991, ScratchBytes: 9007199254740991, OutputBytes: 9007199254740991, PixelCount: 200000000}
	cost, err := CompileShownRetouchAdoptionResources(6000, 4000, owner, limits)
	if err != nil {
		t.Fatal(err)
	}
	if cost.VRAMBytes != 0 || cost.PixelCount != 24000000 || cost.RAMBytes != 64*1024*1024+12345 ||
		cost.ScratchBytes != 24*24000000+2*16384+owner.InputIndexScratchBytes || cost.OutputBytes != 12*24000000+16384+1048576 {
		t.Fatal("copy costs disagree with worker", cost)
	}
	for _, field := range []string{"ram", "scratch", "output", "pixels"} {
		under := cost
		switch field {
		case "ram":
			under.RAMBytes--
		case "scratch":
			under.ScratchBytes--
		case "output":
			under.OutputBytes--
		case "pixels":
			under.PixelCount--
		}
		got, err := CompileShownRetouchAdoptionResources(6000, 4000, owner, under)
		if err == nil || got != (ExposureResourceEstimate{}) {
			t.Fatal("underfunded copy admitted", field)
		}
	}
}
func TestShownRetouchCopyResourcesRejectOverflowAndUnmeasuredOwner(t *testing.T) {
	owner := ExposureOwnerCosts{RuntimeRAMBytes: 1, InputIndexScratchBytes: 16 * 1024 * 1024}
	limits := ExposureResourceEstimate{RAMBytes: 9007199254740991, ScratchBytes: 9007199254740991, OutputBytes: 9007199254740991, PixelCount: 200000000}
	for _, dims := range [][2]int64{{0, 1}, {1, -1}, {200000001, 1}, {200000000, 2}, {9223372036854775807, 9223372036854775807}} {
		if _, err := CompileShownRetouchAdoptionResources(dims[0], dims[1], owner, limits); err == nil {
			t.Fatal("invalid dimensions")
		}
	}
	for _, bad := range []ExposureOwnerCosts{{}, {RuntimeRAMBytes: 9007199254740991, InputIndexScratchBytes: 16 * 1024 * 1024}, {RuntimeRAMBytes: 1, InputIndexScratchBytes: 9007199254740991}} {
		if _, err := CompileShownRetouchAdoptionResources(1, 1, bad, limits); err == nil {
			t.Fatal("invalid owner")
		}
	}
}

func TestShownRetouchCopyBudgetDoesNotRequirePreviewReplayMemory(t *testing.T) {
	owner := ExposureOwnerCosts{RuntimeRAMBytes: 1, InputIndexScratchBytes: 16 * 1024 * 1024}
	limits := ExposureResourceEstimate{RAMBytes: 64*1024*1024 + 1, ScratchBytes: 24*1000000 + 32768 + 16*1024*1024,
		OutputBytes: 12*1000000 + 16384 + 1048576, PixelCount: 1000000}
	if _, err := CompileShownRetouchAdoptionResources(1000, 1000, owner, limits); err != nil {
		t.Fatal("copy budget rejected", err)
	}
	if _, err := CompileExposurePreviewResources(1000, 1000, owner, limits); err == nil {
		t.Fatal("preview replay incorrectly fits copy budget")
	}
}

func TestShownRetouchCopyRejectsAnyDeviceBudget(t *testing.T) {
	owner := ExposureOwnerCosts{RuntimeRAMBytes: 1, InputIndexScratchBytes: 16 * 1024 * 1024}
	for _, vram := range []int64{-1, 1, 10000000000} {
		limits := ExposureResourceEstimate{RAMBytes: 1 << 40, ScratchBytes: 1 << 40, OutputBytes: 1 << 40, PixelCount: 200000000, VRAMBytes: vram}
		got, err := CompileShownRetouchAdoptionResources(1, 1, owner, limits)
		if err != ErrInvalid || got != (ExposureResourceEstimate{}) {
			t.Fatal("device budget accepted", vram, got, err)
		}
	}
}
