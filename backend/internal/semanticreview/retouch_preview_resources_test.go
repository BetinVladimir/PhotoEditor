package semanticreview

import "testing"

func TestRetouchPreviewResourceAdmissionIncludesComputeAndStagingOwners(t *testing.T) {
	owner := ExposureOwnerCosts{RuntimeRAMBytes: 1024 * 1024, InputIndexScratchBytes: 16 * 1024 * 1024}
	limits := ExposureResourceEstimate{RAMBytes: 1 << 40, ScratchBytes: 1 << 40, OutputBytes: 1 << 40, PixelCount: 200000000}
	cost, err := CompileRetouchPreviewResources(6000, 4000, owner, limits)
	if err != nil {
		t.Fatal(err)
	}
	pixels := int64(24000000)
	if cost.RAMBytes < 960*pixels+768*1024*1024+256*4194304+owner.RuntimeRAMBytes ||
		cost.ScratchBytes < 64*pixels+64*1024*1024+4*16384+5*2560*2560+1048576+65536+owner.InputIndexScratchBytes ||
		cost.OutputBytes < 12*pixels+16384+20*2560*2560+4*65536+8*1048576 || cost.VRAMBytes != 0 {
		t.Fatal("underestimated preview")
	}
	for _, field := range []string{"ram", "scratch", "output", "pixels", "negative", "unsafe"} {
		changed := limits
		switch field {
		case "ram":
			changed.RAMBytes = cost.RAMBytes - 1
		case "scratch":
			changed.ScratchBytes = cost.ScratchBytes - 1
		case "output":
			changed.OutputBytes = cost.OutputBytes - 1
		case "pixels":
			changed.PixelCount = pixels - 1
		case "negative":
			changed.VRAMBytes = -1
		case "unsafe":
			changed.RAMBytes = 9007199254740992
		}
		if result, err := CompileRetouchPreviewResources(6000, 4000, owner, changed); err == nil || result != (ExposureResourceEstimate{}) {
			t.Fatal("invalid budget accepted", field)
		}
	}
	for _, size := range [][2]int64{{0, 1}, {1, 0}, {200000001, 1}, {200000000, 2}, {9223372036854775807, 9223372036854775807}} {
		if _, err := CompileRetouchPreviewResources(size[0], size[1], owner, limits); err == nil {
			t.Fatal("invalid dimensions accepted")
		}
	}
}

func TestRetouchPreviewResourceSmallImagePresentationBound(t *testing.T) {
	owner := ExposureOwnerCosts{RuntimeRAMBytes: 1, InputIndexScratchBytes: 16 * 1048576}
	limits := ExposureResourceEstimate{RAMBytes: 1 << 40, ScratchBytes: 1 << 40, OutputBytes: 1 << 40, PixelCount: 200000000}
	cost, err := CompileRetouchPreviewResources(13, 7, owner, limits)
	if err != nil {
		t.Fatal(err)
	}
	if cost.OutputBytes != 32*91+16384+4*65536+8*1048576 {
		t.Fatal("small-image presentation bound", cost)
	}
}


func TestRetouchPreviewScratchAccountsForAllOverlappingFaces(t *testing.T) {
	owner := ExposureOwnerCosts{RuntimeRAMBytes: 1, InputIndexScratchBytes: 16 * 1048576}
	limits := ExposureResourceEstimate{RAMBytes: 1 << 40, ScratchBytes: 1 << 40, OutputBytes: 1 << 40, PixelCount: 200000000}
	for _, pixels := range []int64{1, 1000000, 4194304, 4194305, 200000000} {
		cost, err := CompileRetouchPreviewResources(pixels, 1, owner, limits)
		if err != nil { t.Fatal(err) }
		roi := pixels
		if roi > 4194304 { roi = 4194304 }
		display := pixels
		if display > 2560*2560 { display = 2560*2560 }
		base := 64*pixels + 64*1048576 + 4*16384 + 5*display + 1048576 + 65536 + owner.InputIndexScratchBytes
		if cost.ScratchBytes != base + 64*(16*roi+16384) {
			t.Fatal("incomplete simultaneous face scratch", pixels, cost)
		}
		short := limits
		short.ScratchBytes = cost.ScratchBytes - 1
		if got, err := CompileRetouchPreviewResources(pixels, 1, owner, short); err == nil || got != (ExposureResourceEstimate{}) {
			t.Fatal("accepted undersized scratch", pixels)
		}
	}
}
