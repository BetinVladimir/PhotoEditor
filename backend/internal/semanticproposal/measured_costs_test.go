package semanticproposal

import (
	"math"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"testing"
)

func TestTransferCostsCoverVerifiedMeasuredPeaks(t *testing.T) {
	costs := v2input.SemanticQualifiedCosts{RAMBytes: 101, VRAMBytes: 202, ScratchBytes: 303}
	metrics := v2qualification.Metrics{PeakRSSBytes: 100.5, PeakVRAMBytes: 202, ScratchBytes: 302.1}
	if err := validateTransferMeasuredCosts(costs, metrics); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*v2input.SemanticQualifiedCosts){func(c *v2input.SemanticQualifiedCosts) { c.RAMBytes-- }, func(c *v2input.SemanticQualifiedCosts) { c.VRAMBytes-- }, func(c *v2input.SemanticQualifiedCosts) { c.ScratchBytes-- }} {
		changed := costs
		change(&changed)
		if err := validateTransferMeasuredCosts(changed, metrics); err != v2input.ErrInvalid {
			t.Fatal("understated measured cost accepted", err)
		}
	}
	for _, bad := range []float64{-1, math.NaN(), math.Inf(1), 9007199254740992} {
		changed := metrics
		changed.PeakRSSBytes = bad
		if err := validateTransferMeasuredCosts(costs, changed); err != v2input.ErrInvalid {
			t.Fatal("invalid measured cost accepted", bad, err)
		}
	}
}
