package v2learning

import (
	"context"
	"math"
	"sort"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

type frozenSeriesRegionStats struct {
	RegionID   string     `json:"region_id"`
	MeanRGB    [3]float64 `json:"mean_rgb"`
	MeanY      float64    `json:"mean_y"`
	Coverage   float64    `json:"coverage"`
	Confidence float64    `json:"confidence"`
}

type frozenSeriesStats struct {
	ItemID           uuid.UUID                 `json:"item_id"`
	Image            v2input.Ref               `json:"image"`
	Regions          []frozenSeriesRegionStats `json:"regions"`
	ClippingFraction float64                   `json:"clipping_fraction"`
	Algorithm        string                    `json:"algorithm"`
	Warnings         []string                  `json:"warnings"`
}

func unitSeriesValue(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
}

func validSeriesRegionID(id string) bool {
	if len(id) < 1 || len(id) > 120 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		alnum := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
			c >= '0' && c <= '9'
		if !alnum && (i == 0 || c != '_' && c != '.' && c != ':' && c != '-') {
			return false
		}
	}
	return true
}

// ValidateFrozenSeriesStats checks every body listed by the sealed balance
// Params, not just the selected event. It does not grant object authority:
// callers must obtain every body from a version-pinned Apply source.
func ValidateFrozenSeriesStats(ctx context.Context,
	evidence FrozenSeriesHoldoutEvidence, paramsRaw, seriesRaw []byte,
	statsRaw [][]byte) error {
	if _, err := ValidateFrozenSeriesMaterial(ctx, evidence,
		paramsRaw, seriesRaw); err != nil {
		return err
	}
	var params frozenSeriesParams
	var series frozenSeriesCorrections
	if !decodePinnedSeriesJSON(paramsRaw, evidence.Params.SHA256, &params) ||
		!decodePinnedSeriesJSON(seriesRaw, evidence.Series.SHA256, &series) ||
		len(statsRaw) != len(params.Stats) {
		return ErrInvalidTrainingDraft
	}
	items := make(map[uuid.UUID]bool, len(series.Items))
	for _, item := range series.Items {
		items[item.ItemID] = true
	}
	seenItems := make(map[uuid.UUID]bool, len(statsRaw))
	var common map[string]bool
	for i, raw := range statsRaw {
		if err := ctx.Err(); err != nil {
			return err
		}
		var stat frozenSeriesStats
		if len(raw) > 32768 ||
			!decodePinnedSeriesJSON(raw, params.Stats[i].SHA256, &stat) ||
			stat.ItemID == uuid.Nil || !items[stat.ItemID] ||
			seenItems[stat.ItemID] || !validManifestRef(stat.Image) ||
			stat.Algorithm != "seriesmatch_v1" ||
			!unitSeriesValue(stat.ClippingFraction) ||
			len(stat.Regions) < 1 || len(stat.Regions) > 32 ||
			len(stat.Warnings) > 2 {
			return ErrInvalidTrainingDraft
		}
		if stat.ItemID == evidence.ItemID && stat.Image != evidence.StageImage {
			return ErrInvalidTrainingDraft
		}
		seenItems[stat.ItemID] = true
		regions := make(map[string]bool, len(stat.Regions))
		for _, region := range stat.Regions {
			if !validSeriesRegionID(region.RegionID) ||
				regions[region.RegionID] || !unitSeriesValue(region.MeanY) ||
				!unitSeriesValue(region.Coverage) ||
				!unitSeriesValue(region.Confidence) {
				return ErrInvalidTrainingDraft
			}
			for _, channel := range region.MeanRGB {
				if !unitSeriesValue(channel) {
					return ErrInvalidTrainingDraft
				}
			}
			regions[region.RegionID] = true
		}
		seenWarnings := make(map[string]bool, len(stat.Warnings))
		for _, warning := range stat.Warnings {
			if warning != "low_region_coverage" &&
				warning != "low_mask_confidence" || seenWarnings[warning] {
				return ErrInvalidTrainingDraft
			}
			seenWarnings[warning] = true
		}
		if common == nil {
			common = regions
		} else {
			for region := range common {
				if !regions[region] {
					delete(common, region)
				}
			}
		}
	}
	if len(seenItems) != len(series.Items) ||
		len(common) != len(series.CommonRegionIDs) {
		return ErrInvalidTrainingDraft
	}
	shared := make([]string, 0, len(common))
	for region := range common {
		shared = append(shared, region)
	}
	sort.Strings(shared)
	for i, region := range shared {
		if region != series.CommonRegionIDs[i] {
			return ErrInvalidTrainingDraft
		}
	}
	return ctx.Err()
}
