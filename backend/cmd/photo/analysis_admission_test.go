package main

import (
	"math"
	"testing"
)

func TestAnalysisDefaultVRAM(t *testing.T) {
	if got := analysisDefaultVRAM("mps"); got != math.MaxInt64 {
		t.Fatalf("Metal has a fixed admission ceiling: %d", got)
	}
	if got := analysisDefaultVRAM("cuda"); got != 10_000_000_000 {
		t.Fatalf("CUDA default=%d, want decimal 10 GB", got)
	}
	if got := analysisDefaultVRAM("cpu"); got != 0 {
		t.Fatalf("CPU reserves GPU memory: %d", got)
	}
}

func TestAnalysisMemoryRetainsExplicitOperatorBudget(t *testing.T) {
	t.Setenv("PHOTO_V22_VRAM_BYTES", "16000000000")
	got, err := analysisMemory("PHOTO_V22_VRAM_BYTES", analysisDefaultVRAM("mps"))
	if err != nil || got != 16_000_000_000 {
		t.Fatalf("explicit Metal deployment budget ignored: %d %v", got, err)
	}
	t.Setenv("PHOTO_V22_VRAM_BYTES", "-1")
	if _, err := analysisMemory("PHOTO_V22_VRAM_BYTES", analysisDefaultVRAM("mps")); err == nil {
		t.Fatal("negative budget accepted")
	}
}
