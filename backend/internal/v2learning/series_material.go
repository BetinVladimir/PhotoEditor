package v2learning

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type frozenSeriesParams struct {
	SceneID         string        `json:"scene_id"`
	Stats           []v2input.Ref `json:"stats"`
	ReferenceItemID *uuid.UUID    `json:"reference_item_id"`
	EVLimit         float64       `json:"ev_limit"`
	GainMin         float64       `json:"gain_min"`
	GainMax         float64       `json:"gain_max"`
	MinConfidence   float64       `json:"min_confidence"`
	Algorithm       string        `json:"algorithm"`
}

// FrozenSeriesItemCorrection is the sole item-specific value selected from
// the canonical aggregate. It is copied, so later callers cannot mutate the
// decoded source document through shared slices.
type FrozenSeriesItemCorrection struct {
	ItemID       uuid.UUID  `json:"item_id"`
	RawEV        float64    `json:"raw_ev"`
	RawGains     [3]float64 `json:"raw_gains"`
	AppliedEV    float64    `json:"applied_ev"`
	AppliedGains [3]float64 `json:"applied_gains"`
	Confidence   float64    `json:"confidence"`
	Accepted     bool       `json:"accepted"`
	Reason       string     `json:"reason"`
}

type frozenSeriesCorrections struct {
	SceneID         string                       `json:"scene_id"`
	ReferenceItemID uuid.UUID                    `json:"reference_item_id"`
	CommonRegionIDs []string                     `json:"common_region_ids"`
	Items           []FrozenSeriesItemCorrection `json:"items"`
	Algorithm       string                       `json:"algorithm"`
	Warnings        []string                     `json:"warnings"`
}

func decodePinnedSeriesJSON(raw []byte, expectedSHA string, value any) bool {
	if len(raw) < 2 || len(raw) > 1<<20 || !validSHA(expectedSHA) {
		return false
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != expectedSHA {
		return false
	}
	canonicalRaw, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(canonicalRaw, raw) {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(value) == nil
}

func validSeriesCorrection(item FrozenSeriesItemCorrection) bool {
	if item.ItemID == uuid.Nil || math.IsNaN(item.RawEV) ||
		item.RawEV < -10 || item.RawEV > 10 ||
		math.IsNaN(item.AppliedEV) ||
		item.AppliedEV < -.35 || item.AppliedEV > .35 ||
		math.IsNaN(item.Confidence) ||
		item.Confidence < 0 || item.Confidence > 1 {
		return false
	}
	for i := 0; i < 3; i++ {
		if math.IsNaN(item.RawGains[i]) || item.RawGains[i] < .01 ||
			item.RawGains[i] > 100 || math.IsNaN(item.AppliedGains[i]) ||
			item.AppliedGains[i] < .95 || item.AppliedGains[i] > 1.05 {
			return false
		}
	}
	switch item.Reason {
	case "applied", "reference", "low_confidence", "no_common_regions",
		"residual_not_improved", "clipping_regression":
	default:
		return false
	}
	if !item.Accepted && (item.AppliedEV != 0 ||
		item.AppliedGains != [3]float64{1, 1, 1}) {
		return false
	}
	return true
}

// ValidateFrozenSeriesMaterial checks the two exact SQL-located object bodies
// and selects one item from the canonical aggregate. Source authority and
// object-version locators must be rechecked by the caller around this call.
func ValidateFrozenSeriesMaterial(ctx context.Context,
	evidence FrozenSeriesHoldoutEvidence, paramsRaw, seriesRaw []byte) (FrozenSeriesItemCorrection, error) {
	if err := ctx.Err(); err != nil {
		return FrozenSeriesItemCorrection{}, err
	}
	if !validFrozenSeriesHoldout(evidence) {
		return FrozenSeriesItemCorrection{}, ErrInvalidTrainingDraft
	}
	var params frozenSeriesParams
	var series frozenSeriesCorrections
	if !decodePinnedSeriesJSON(paramsRaw, evidence.Params.SHA256, &params) ||
		!decodePinnedSeriesJSON(seriesRaw, evidence.Series.SHA256, &series) ||
		params.SceneID == "" || len(params.SceneID) > 120 ||
		params.SceneID != series.SceneID ||
		params.Algorithm != "seriesmatch_v1" ||
		series.Algorithm != "seriesmatch_v1" ||
		params.EVLimit != .35 || params.GainMin != .95 ||
		params.GainMax != 1.05 || params.MinConfidence != .75 ||
		len(params.Stats) < 2 || len(params.Stats) > 2000 ||
		len(series.Items) != len(params.Stats) ||
		series.ReferenceItemID == uuid.Nil ||
		len(series.CommonRegionIDs) > 32 || len(series.Warnings) > 2 {
		return FrozenSeriesItemCorrection{}, ErrInvalidTrainingDraft
	}
	if params.ReferenceItemID != nil &&
		*params.ReferenceItemID != series.ReferenceItemID {
		return FrozenSeriesItemCorrection{}, ErrInvalidTrainingDraft
	}
	seenStats := make(map[uuid.UUID]bool, len(params.Stats))
	for _, stat := range params.Stats {
		if !validManifestRef(stat) || seenStats[stat.ArtifactID] {
			return FrozenSeriesItemCorrection{}, ErrInvalidTrainingDraft
		}
		seenStats[stat.ArtifactID] = true
	}
	seenRegions := make(map[string]bool, len(series.CommonRegionIDs))
	for _, region := range series.CommonRegionIDs {
		if region == "" || len(region) > 120 || seenRegions[region] {
			return FrozenSeriesItemCorrection{}, ErrInvalidTrainingDraft
		}
		seenRegions[region] = true
	}
	for _, warning := range series.Warnings {
		if warning != "no_common_regions" && warning != "items_skipped" {
			return FrozenSeriesItemCorrection{}, ErrInvalidTrainingDraft
		}
	}
	seenItems := make(map[uuid.UUID]bool, len(series.Items))
	var selected FrozenSeriesItemCorrection
	for _, item := range series.Items {
		if !validSeriesCorrection(item) || seenItems[item.ItemID] {
			return FrozenSeriesItemCorrection{}, ErrInvalidTrainingDraft
		}
		seenItems[item.ItemID] = true
		if item.ItemID == evidence.ItemID {
			selected = item
		}
	}
	if !seenItems[evidence.ItemID] || !seenItems[series.ReferenceItemID] {
		return FrozenSeriesItemCorrection{}, ErrInvalidTrainingDraft
	}
	if err := ctx.Err(); err != nil {
		return FrozenSeriesItemCorrection{}, err
	}
	return selected, nil
}
