package main

import "testing"

func TestAnalysisDefaultVRAM(t *testing.T) {
	if got := analysisDefaultVRAM("mps"); got != 10_000_000_000 {
		t.Fatalf("Metal default = %d, want 10 GB", got)
	}
	if got := analysisDefaultVRAM("cuda"); got != 10<<30 {
		t.Fatalf("CUDA default = %d, want 10 GiB", got)
	}
}
