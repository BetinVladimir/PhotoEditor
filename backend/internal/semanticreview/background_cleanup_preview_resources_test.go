package semanticreview

import "testing"

func TestBackgroundCleanupMemoryBackendPolicy(t *testing.T) {
	owner := ExposureOwnerCosts{RuntimeRAMBytes: 1, InputIndexScratchBytes: 16 * 1024 * 1024}
	limits := ExposureResourceEstimate{RAMBytes: 1 << 40, VRAMBytes: 1 << 40, ScratchBytes: 1 << 40, OutputBytes: 1 << 40, PixelCount: 200000000}
	for _, test := range []struct {
		backend string
		device  int64
		valid   bool
	}{
		{"cpu", 0, true}, {"cpu", 1, false}, {"cuda", 10000000000, true}, {"cuda", 10000000001, false}, {"mps", 12000000000, true}, {"mps", 0, false}, {"unknown", 1, false},
	} {
		cost, err := CompileBackgroundCleanupPreviewResources(100, 100, owner, limits, test.backend, test.device)
		if (err == nil) != test.valid {
			t.Fatalf("%s %d: %v", test.backend, test.device, err)
		}
		if err == nil && cost.VRAMBytes != test.device {
			t.Fatal("measured device budget lost")
		}
	}
	cost, err := CompileBackgroundCleanupPreviewResources(100, 100, owner, limits, "mps", 12000000000)
	if err != nil {
		t.Fatal(err)
	}
	limits.VRAMBytes = cost.VRAMBytes - 1
	if _, err := CompileBackgroundCleanupPreviewResources(100, 100, owner, limits, "mps", 12000000000); err == nil {
		t.Fatal("insufficient device budget admitted")
	}
}

func TestBackgroundCleanupResourceLimitsMatchExactMeasuredCost(t *testing.T) {
	owner := ExposureOwnerCosts{RuntimeRAMBytes: 1, InputIndexScratchBytes: 16 * 1024 * 1024}
	generous := ExposureResourceEstimate{RAMBytes: 1 << 40, VRAMBytes: 1 << 40, ScratchBytes: 1 << 40, OutputBytes: 1 << 40, PixelCount: 200000000}
	cost, err := CompileBackgroundCleanupPreviewResources(100, 80, owner, generous, "mps", 12000000000)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = CompileBackgroundCleanupPreviewResources(100, 80, owner, cost, "mps", 12000000000); err != nil {
		t.Fatal("exact limits rejected", err)
	}
	for name, mutate := range map[string]func(*ExposureResourceEstimate){
		"ram":     func(c *ExposureResourceEstimate) { c.RAMBytes-- },
		"device":  func(c *ExposureResourceEstimate) { c.VRAMBytes-- },
		"scratch": func(c *ExposureResourceEstimate) { c.ScratchBytes-- },
		"output":  func(c *ExposureResourceEstimate) { c.OutputBytes-- },
		"pixels":  func(c *ExposureResourceEstimate) { c.PixelCount-- },
	} {
		t.Run(name, func(t *testing.T) {
			limit := cost
			mutate(&limit)
			if _, err := CompileBackgroundCleanupPreviewResources(100, 80, owner, limit, "mps", 12000000000); err == nil {
				t.Fatal("insufficient limit admitted")
			}
		})
	}
	for _, size := range [][2]int64{{0, 80}, {100, 0}, {200000001, 1}, {200000000, 2}, {-1, 80}} {
		if _, err := CompileBackgroundCleanupPreviewResources(size[0], size[1], owner, generous, "cpu", 0); err == nil {
			t.Fatal("invalid pixel count admitted", size)
		}
	}
}

// These scenarios exercise the JSON-safe integer boundary independently of
// physical GPU qualification. No hardware or database is accessed.
func TestBackgroundCleanupResourceIntegerBoundaries(t *testing.T) {
	const safe int64 = 9007199254740991
	owner := ExposureOwnerCosts{RuntimeRAMBytes: 1, InputIndexScratchBytes: 16 * 1024 * 1024}
	limits := ExposureResourceEstimate{RAMBytes: safe, VRAMBytes: safe, ScratchBytes: safe, OutputBytes: safe, PixelCount: safe}
	cost, err := CompileBackgroundCleanupPreviewResources(1, 1, owner, limits, "mps", safe)
	if err != nil || cost.VRAMBytes != safe || cost.PixelCount != 1 {
		t.Fatal("safe measured Metal boundary rejected", cost, err)
	}
	for name, mutate := range map[string]func(*ExposureOwnerCosts, *ExposureResourceEstimate){
		"runtime_overflow":       func(o *ExposureOwnerCosts, _ *ExposureResourceEstimate) { o.RuntimeRAMBytes = safe },
		"scratch_overflow":       func(o *ExposureOwnerCosts, _ *ExposureResourceEstimate) { o.InputIndexScratchBytes = safe },
		"negative_runtime":       func(o *ExposureOwnerCosts, _ *ExposureResourceEstimate) { o.RuntimeRAMBytes = -1 },
		"index_below_floor":      func(o *ExposureOwnerCosts, _ *ExposureResourceEstimate) { o.InputIndexScratchBytes-- },
		"unsafe_ram_limit":       func(_ *ExposureOwnerCosts, l *ExposureResourceEstimate) { l.RAMBytes = safe + 1 },
		"unsafe_device_limit":    func(_ *ExposureOwnerCosts, l *ExposureResourceEstimate) { l.VRAMBytes = safe + 1 },
		"unsafe_scratch_limit":   func(_ *ExposureOwnerCosts, l *ExposureResourceEstimate) { l.ScratchBytes = safe + 1 },
		"unsafe_output_limit":    func(_ *ExposureOwnerCosts, l *ExposureResourceEstimate) { l.OutputBytes = safe + 1 },
		"unsafe_pixel_limit":     func(_ *ExposureOwnerCosts, l *ExposureResourceEstimate) { l.PixelCount = safe + 1 },
		"negative_ram_limit":     func(_ *ExposureOwnerCosts, l *ExposureResourceEstimate) { l.RAMBytes = -1 },
		"negative_device_limit":  func(_ *ExposureOwnerCosts, l *ExposureResourceEstimate) { l.VRAMBytes = -1 },
		"negative_scratch_limit": func(_ *ExposureOwnerCosts, l *ExposureResourceEstimate) { l.ScratchBytes = -1 },
		"negative_output_limit":  func(_ *ExposureOwnerCosts, l *ExposureResourceEstimate) { l.OutputBytes = -1 },
		"negative_pixel_limit":   func(_ *ExposureOwnerCosts, l *ExposureResourceEstimate) { l.PixelCount = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			o, l := owner, limits
			mutate(&o, &l)
			got, err := CompileBackgroundCleanupPreviewResources(1, 1, o, l, "cpu", 0)
			if err == nil || got != (ExposureResourceEstimate{}) {
				t.Fatal("invalid accounting escaped with a usable estimate", got, err)
			}
		})
	}
	for _, device := range []int64{-1, safe + 1} {
		got, err := CompileBackgroundCleanupPreviewResources(1, 1, owner, limits, "mps", device)
		if err == nil || got != (ExposureResourceEstimate{}) {
			t.Fatal("unsafe measured device bytes admitted", device, got, err)
		}
	}
}
