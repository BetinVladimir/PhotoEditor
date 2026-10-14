package semanticproposal

import (
	"math"
	"testing"

	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

func TestTransferRuntimeCostsRoundUpAndRejectUnsafeMeasurements(t *testing.T) {
	metrics := v2qualification.Metrics{PeakRSSBytes: 100.1, PeakVRAMBytes: 200.2, ScratchBytes: 300.3}
	expected := v2input.SemanticQualifiedCosts{RAMBytes: 101, VRAMBytes: 201, ScratchBytes: 301}
	if costs, err := transferCostsFromVerifiedMetrics(metrics); err != nil || costs != expected {
		t.Fatal("physical peaks rounded down", costs, err)
	}
	for _, bad := range []float64{-1, math.NaN(), math.Inf(1), math.Inf(-1), 9007199254740992} {
		for _, field := range []string{"ram", "vram", "scratch"} {
			changed := metrics
			switch field {
			case "ram":
				changed.PeakRSSBytes = bad
			case "vram":
				changed.PeakVRAMBytes = bad
			case "scratch":
				changed.ScratchBytes = bad
			}
			if costs, err := transferCostsFromVerifiedMetrics(changed); err == nil || costs != (v2input.SemanticQualifiedCosts{}) {
				t.Fatal("unsafe physical metric exposed costs", field)
			}
		}
	}
	if costs, err := transferCostsFromVerifiedMetrics(v2qualification.Metrics{}); err != nil || costs != (v2input.SemanticQualifiedCosts{}) {
		t.Fatal("zero additional metrics refused", err)
	}
}
