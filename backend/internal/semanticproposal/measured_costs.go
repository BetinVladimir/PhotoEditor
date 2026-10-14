package semanticproposal

import (
	"math"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Installed additional costs conservatively cover observed report peaks.
// These lower bounds do not establish capacity at other resolutions or replace
// device qualification, owner overhead estimates or aggregate concurrency limits.
func validateTransferMeasuredCosts(cost v2input.SemanticQualifiedCosts, metrics v2qualification.Metrics) error {
	for _, pair := range []struct {
		installed int64
		measured  float64
	}{
		{cost.RAMBytes, metrics.PeakRSSBytes},
		{cost.VRAMBytes, metrics.PeakVRAMBytes},
		{cost.ScratchBytes, metrics.ScratchBytes},
	} {
		if pair.installed < 0 || pair.installed > 9007199254740991 || math.IsNaN(pair.measured) || math.IsInf(pair.measured, 0) || pair.measured < 0 || pair.measured > 9007199254740991 || float64(pair.installed) < math.Ceil(pair.measured) {
			return v2input.ErrInvalid
		}
	}
	return nil
}

// Integer compiler inputs round physical peaks up, never down. Profile-specific
// owner/transport overhead is still added separately by the task compiler.
func transferCostsFromVerifiedMetrics(metrics v2qualification.Metrics) (v2input.SemanticQualifiedCosts, error) {
	var zero v2input.SemanticQualifiedCosts
	values := []float64{metrics.PeakRSSBytes, metrics.PeakVRAMBytes, metrics.ScratchBytes}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 9007199254740991 {
			return zero, v2input.ErrInvalid
		}
	}
	costs := v2input.SemanticQualifiedCosts{RAMBytes: int64(math.Ceil(values[0])), VRAMBytes: int64(math.Ceil(values[1])), ScratchBytes: int64(math.Ceil(values[2]))}
	if err := validateTransferMeasuredCosts(costs, metrics); err != nil {
		return zero, err
	}
	return costs, nil
}
