package v2learning

import (
	"encoding/json"

	"photoeditor/backend/internal/review"
)

// validateFrozenRegionBindings rejects missing, extra, or ambiguous masks
// before the Evaluate task is admitted. The worker repeats the check against
// its fully validated StyleV2 contract.
func validateFrozenRegionBindings(styleRaw []byte,
	bindings []TrainingRegionMask, masks map[string]review.MaskRef) error {
	var style struct {
		Extensions struct {
			RegionAdjustments []struct {
				RegionID string `json:"region_id"`
				Selector string `json:"selector"`
			} `json:"region_adjustments"`
		} `json:"extensions"`
	}
	if json.Unmarshal(styleRaw, &style) != nil ||
		len(style.Extensions.RegionAdjustments) != len(bindings) ||
		len(masks) != len(bindings) || len(bindings) > 128 {
		return ErrInvalidTrainingDraft
	}
	seen := make(map[string]bool, len(bindings))
	for _, rule := range style.Extensions.RegionAdjustments {
		mask, okay := masks[rule.RegionID]
		if !okay || seen[rule.RegionID] || rule.RegionID == "" ||
			mask.Selector != rule.Selector || mask.Confidence < .75 {
			return ErrInvalidTrainingDraft
		}
		seen[rule.RegionID] = true
	}
	return nil
}
