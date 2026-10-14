package v2draft

import (
	"encoding/json"
	"math"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func correctionBindings(raw []byte, binding v2input.SessionVersionBinding) ([]v2input.SessionVersionBinding, error) {
	if err := ValidateCorrectionPlan(raw, binding); err != nil {
		return nil, err
	}
	var plan struct {
		Items []struct {
			Binding v2input.SessionVersionBinding `json:"binding"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &plan); err != nil {
		return nil, err
	}
	items := make([]v2input.SessionVersionBinding, len(plan.Items))
	for i, item := range plan.Items {
		items[i] = item.Binding
	}
	return items, nil
}

func correctionDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func validCorrectionVector(raw []byte) bool {
	_, err := DecodeCorrectionVector(raw)
	return err == nil
}

type CorrectionVector struct {
	EV              float64 `json:"ev"`
	LogR            float64 `json:"log_r"`
	LogG            float64 `json:"log_g"`
	LogB            float64 `json:"log_b"`
	SaturationDelta float64 `json:"saturation_delta"`
	RetouchDelta    float64 `json:"retouch_delta"`
}

func (v CorrectionVector) valid() bool {
	return !math.IsNaN(v.EV) && !math.IsInf(v.EV, 0) && math.Abs(v.EV) <= 1 &&
		!math.IsNaN(v.LogR) && !math.IsInf(v.LogR, 0) && math.Abs(v.LogR) <= 0.223144 &&
		!math.IsNaN(v.LogG) && !math.IsInf(v.LogG, 0) && math.Abs(v.LogG) <= 0.223144 &&
		!math.IsNaN(v.LogB) && !math.IsInf(v.LogB, 0) && math.Abs(v.LogB) <= 0.223144 &&
		!math.IsNaN(v.SaturationDelta) && !math.IsInf(v.SaturationDelta, 0) && math.Abs(v.SaturationDelta) <= 0.15 &&
		!math.IsNaN(v.RetouchDelta) && !math.IsInf(v.RetouchDelta, 0) && math.Abs(v.RetouchDelta) <= 0.15
}

// DecodeCorrectionVector rejects unknown or missing coordinates before a
// correction is used to freeze a recipe.
func DecodeCorrectionVector(raw []byte) (CorrectionVector, error) {
	_, ok := exactFields(raw, "ev", "log_r", "log_g", "log_b",
		"saturation_delta", "retouch_delta")
	if !ok {
		return CorrectionVector{}, v2input.ErrInvalid
	}
	var vector CorrectionVector
	if json.Unmarshal(raw, &vector) != nil || !vector.valid() {
		return CorrectionVector{}, v2input.ErrInvalid
	}
	return vector, nil
}

// SumCorrectionVectors checks the final six-dimensional vector before it can
// become a FrozenRecipe effective_delta. Inputs have already been validated.
func SumCorrectionVectors(parts ...CorrectionVector) (CorrectionVector, error) {
	var total CorrectionVector
	for _, part := range parts {
		if !part.valid() {
			return CorrectionVector{}, v2input.ErrInvalid
		}
		total.EV += part.EV
		total.LogR += part.LogR
		total.LogG += part.LogG
		total.LogB += part.LogB
		total.SaturationDelta += part.SaturationDelta
		total.RetouchDelta += part.RetouchDelta
	}
	if !total.valid() {
		return CorrectionVector{}, v2input.ErrInvalid
	}
	return total, nil
}

// CurrentCorrection extracts the one current frame. Locked and skipped
// frames must be identity; their stored vector cannot silently alter approval.
func CurrentCorrection(raw []byte, binding v2input.SessionVersionBinding) (CorrectionVector, error) {
	if err := ValidateCorrectionPlan(raw, binding); err != nil {
		return CorrectionVector{}, err
	}
	var plan struct {
		Items []struct {
			Binding     v2input.SessionVersionBinding `json:"binding"`
			Delta       CorrectionVector              `json:"delta"`
			Disposition string                        `json:"disposition"`
		} `json:"items"`
	}
	if json.Unmarshal(raw, &plan) != nil {
		return CorrectionVector{}, v2input.ErrInvalid
	}
	for _, item := range plan.Items {
		if item.Binding == binding {
			if item.Disposition != "apply" {
				return CorrectionVector{}, nil
			}
			return item.Delta, nil
		}
	}
	return CorrectionVector{}, v2input.ErrInvalid
}

// ValidateCorrectionPlan requires the current source exactly once in an
// ordered, bounded scene group using original snapshot ordinals. Other items belong to the same
// project through artifact provenance checks in the catalog.
func ValidateCorrectionPlan(raw []byte, binding v2input.SessionVersionBinding) error {
	if len(raw) < 2 || len(raw) > maxDocumentJSON {
		return v2input.ErrInvalid
	}
	fields, ok := exactFields(raw, "schema_version", "snapshot_digest", "items")
	if !ok {
		return v2input.ErrInvalid
	}
	var version int
	var snapshot string
	var items []json.RawMessage
	if json.Unmarshal(fields["schema_version"], &version) != nil || version != 1 ||
		json.Unmarshal(fields["snapshot_digest"], &snapshot) != nil ||
		!correctionDigest(snapshot) ||
		json.Unmarshal(fields["items"], &items) != nil ||
		len(items) < 1 || len(items) > 5000 {
		return v2input.ErrInvalid
	}
	matched := false
	seen := make(map[uuid.UUID]bool, len(items))
	previousOrdinal := -1
	for _, rawItem := range items {
		item, valid := exactFields(rawItem, "binding", "ordinal", "delta", "disposition", "reason")
		if !valid || !validCorrectionVector(item["delta"]) {
			return v2input.ErrInvalid
		}
		bindingFields, valid := exactFields(item["binding"], "asset_version_id", "source", "canvas_hash")
		if !valid {
			return v2input.ErrInvalid
		}
		if _, valid = layerRef(bindingFields["source"]); !valid {
			return v2input.ErrInvalid
		}
		var source v2input.SessionVersionBinding
		var index int
		var disposition string
		if json.Unmarshal(item["binding"], &source) != nil || source.AssetVersionID == uuid.Nil ||
			!correctionDigest(source.CanvasHash) || seen[source.AssetVersionID] ||
			json.Unmarshal(item["ordinal"], &index) != nil || index < 0 || index > 4999 || index <= previousOrdinal ||
			json.Unmarshal(item["disposition"], &disposition) != nil ||
			(disposition != "apply" && disposition != "no_change" && disposition != "manual_locked") {
			return v2input.ErrInvalid
		}
		if disposition != "apply" {
			var delta CorrectionVector
			if json.Unmarshal(item["delta"], &delta) != nil || delta != (CorrectionVector{}) {
				return v2input.ErrInvalid
			}
		}
		if string(item["reason"]) != "null" {
			var reason string
			if json.Unmarshal(item["reason"], &reason) != nil || len(reason) < 1 || len(reason) > 80 {
				return v2input.ErrInvalid
			}
		}
		seen[source.AssetVersionID] = true
		previousOrdinal = index
		if source == binding {
			matched = true
		}
	}
	if !matched {
		return v2input.ErrInvalid
	}
	return nil
}
