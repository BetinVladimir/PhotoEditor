package v2learning

import (
	"encoding/json"
 "math"

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
 bound:=make(map[string]bool,len(bindings))
 for _,binding:=range bindings {
  if len(binding.RegionID)<1||len(binding.RegionID)>120||bound[binding.RegionID]{return ErrInvalidTrainingDraft}
  bound[binding.RegionID]=true
 }
	seen := make(map[string]bool, len(bindings))
	for _, rule := range style.Extensions.RegionAdjustments {
		mask, okay := masks[rule.RegionID]
		if !okay || !bound[rule.RegionID] || seen[rule.RegionID] || rule.RegionID == "" ||
			mask.Selector != rule.Selector || mask.Protected ||
   math.IsNaN(mask.Confidence) || math.IsInf(mask.Confidence,0) ||
   mask.Confidence < .75 || mask.Confidence > 1 {
			return ErrInvalidTrainingDraft
		}
		seen[rule.RegionID] = true
	}
	return nil
}
