package semanticreview

import "testing"

func TestCompleteExposureResourcesAndBudgetDenial(t *testing.T) {
	owner := ExposureOwnerCosts{RuntimeRAMBytes: 128 * 1024 * 1024, InputIndexScratchBytes: 16 * 1024 * 1024}
	limits := ExposureResourceEstimate{RAMBytes: 32 * 1024 * 1024 * 1024, ScratchBytes: 2 * 1024 * 1024 * 1024, OutputBytes: 1024 * 1024 * 1024, PixelCount: 24000000}
	got, err := CompileExposureResources(6000, 4000, owner, limits)
	if err != nil || got.RAMBytes != 4963108864+owner.RuntimeRAMBytes || got.ScratchBytes != 480000000+3*16384+owner.InputIndexScratchBytes || got.OutputBytes != 288000000+16384+3*1048576 || got.VRAMBytes != 0 {
		t.Fatal(got, err)
	}
	exact := got
	if _, err := CompileExposureResources(6000, 4000, owner, exact); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"ram", "scratch", "output", "pixels"} {
		insufficient := exact
		switch field {
		case "ram":
			insufficient.RAMBytes--
		case "scratch":
			insufficient.ScratchBytes--
		case "output":
			insufficient.OutputBytes--
		case "pixels":
			insufficient.PixelCount--
		}
		if _, err := CompileExposureResources(6000, 4000, owner, insufficient); err == nil {
			t.Fatal("insufficient budget admitted", field)
		}
	}
	if _, err := CompileExposureResources(200000000, 1, owner, limits); err == nil {
		t.Fatal("200MP admitted to 32GiB")
	}
	for _, bad := range []ExposureOwnerCosts{{0, 16 * 1024 * 1024}, {1, 0}, {9007199254740991, 16 * 1024 * 1024}, {1, 9007199254740991}} {
		if _, err := CompileExposureResources(6000, 4000, bad, limits); err == nil {
			t.Fatal("invalid owner costs admitted")
		}
	}
}
