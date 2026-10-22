package v2render

import (
	"math"
	"photoeditor/backend/internal/v2qualification"
	"testing"
)

func TestAuthoredDefectMeasuredCostAdmissionRoundsUp(t *testing.T) {
	for _, row := range []struct {
		name                 string
		ram, scratch         float64
		wantRAM, wantScratch int64
	}{
		{"zero", 0, 0, 0, 0},
		{"fractional", 0.01, 1.01, 1, 2},
		{"integer", 4096, 8192, 4096, 8192},
		{"largest-safe-integer", 9007199254740991, 9007199254740991, 9007199254740991, 9007199254740991},
	} {
		t.Run(row.name, func(t *testing.T) {
			got, err := authoredDefectMeasuredCosts(v2qualification.Metrics{PeakRSSBytes: row.ram, ScratchBytes: row.scratch})
			if err != nil || got.RAMBytes != row.wantRAM || got.ScratchBytes != row.wantScratch || got.VRAMBytes != 0 {
				t.Fatalf("incorrect conservative resource admission: %+v %v", got, err)
			}
		})
	}
}

func TestAuthoredDefectMeasuredCostAdmissionRefusesUnsafeMeasurements(t *testing.T) {
	for _, field := range []string{"rss", "scratch", "vram"} {
		for _, value := range []float64{-1, math.NaN(), math.Inf(1), math.Inf(-1), 9007199254740992} {
			metrics := v2qualification.Metrics{PeakRSSBytes: 1, ScratchBytes: 1}
			switch field {
			case "rss":
				metrics.PeakRSSBytes = value
			case "scratch":
				metrics.ScratchBytes = value
			case "vram":
				metrics.PeakVRAMBytes = value
			}
			if _, err := authoredDefectMeasuredCosts(metrics); err == nil {
				t.Fatalf("unsafe %s measurement admitted: %v", field, value)
			}
		}
	}
	for _, value := range []float64{0.01, 1, 4096} {
		if _, err := authoredDefectMeasuredCosts(v2qualification.Metrics{PeakRSSBytes: 1, ScratchBytes: 1, PeakVRAMBytes: value}); err == nil {
			t.Fatalf("CPU qualification relabelled nonzero VRAM: %v", value)
		}
	}
}
