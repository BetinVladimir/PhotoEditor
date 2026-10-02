package v2learning

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"strconv"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// loadScalarOverrides accepts ordered, source-bound OverrideV1 documents only
// when they replace only global gains/saturation or beauty smoothing.
// A change to curves, LUT or any other module is not a
// parameter-only training example.
func (s TrainingRecipeSource) loadScalarOverrides(ctx context.Context,
	who auth.Identity, tenantID uuid.UUID, refs []v2input.Ref,
	styleRef v2input.Ref,
	style v2draft.StyleBaseline, session *v2draft.ResolvedCorrection,
	learning *v2draft.CorrectionVector) ([]v2draft.ResolvedCorrection, error) {
	result := make([]v2draft.ResolvedCorrection, 0, len(refs))
	if len(refs) == 0 {
		return result, nil
	}
	styleObject, err := s.Catalog.Lookup(ctx, who, tenantID, v2draft.LayerRef{
		Path: "EditingRecipeV3.base_style", Contract: "StyleV2", Ref: styleRef})
	if err != nil {
		return nil, err
	}
	styleBody, err := ReadFrozenRootDocument(ctx, s.Objects, styleObject, styleRef)
	if err != nil {
		return nil, err
	}
	var base struct {
		Global map[string]json.RawMessage `json:"global"`
		Beauty map[string]json.RawMessage `json:"beauty"`
	}
	if json.Unmarshal(styleBody, &base) != nil || len(base.Global) == 0 ||
		len(base.Beauty) == 0 {
		return nil, ErrInvalidTrainingDraft
	}
	var current v2draft.CorrectionVector
	if session != nil {
		current = session.Vector
	}
	if learning != nil {
		current, err = v2draft.SumCorrectionVectors(current, *learning)
		if err != nil {
			return nil, ErrInvalidTrainingExample
		}
	}
	for i, ref := range refs {
		if !validManifestRef(ref) {
			return nil, ErrInvalidTrainingDraft
		}
		object, err := s.Catalog.Lookup(ctx, who, tenantID, v2draft.LayerRef{
			Path:     "EditingRecipeV3.overrides[" + strconv.Itoa(i) + "]",
			Contract: "Override", Ref: ref})
		if err != nil {
			return nil, err
		}
		body, err := ReadFrozenRootDocument(ctx, s.Objects, object, ref)
		if err != nil {
			return nil, err
		}
		var doc struct {
			ObjectID             *string                    `json:"object_id"`
			Modules              map[string]json.RawMessage `json:"modules"`
			AcceptedCandidateIDs []string                   `json:"accepted_candidate_ids"`
			RejectedCandidateIDs []string                   `json:"rejected_candidate_ids"`
			MaskRefs             []json.RawMessage          `json:"mask_refs"`
			ProtectedPolygons    []json.RawMessage          `json:"protected_polygons"`
			ManualLandmarks      json.RawMessage            `json:"manual_landmarks"`
		}
		if json.Unmarshal(body, &doc) != nil || doc.ObjectID != nil ||
			len(doc.Modules) < 1 ||
			len(doc.Modules) > 2 ||
			len(doc.AcceptedCandidateIDs) != 0 ||
			len(doc.RejectedCandidateIDs) != 0 || len(doc.MaskRefs) != 0 ||
			len(doc.ProtectedPolygons) != 0 ||
			(len(doc.ManualLandmarks) != 0 && string(doc.ManualLandmarks) != "null") {
			return nil, ErrInvalidTrainingExample
		}
		target := current
		if globalBody, ok := doc.Modules["global"]; ok {
			var global map[string]json.RawMessage
			if json.Unmarshal(globalBody, &global) != nil ||
				len(global) != len(base.Global) {
				return nil, ErrInvalidTrainingExample
			}
			for name, baseline := range base.Global {
				if name != "gains_rgb" && name != "saturation" &&
					!bytes.Equal(global[name], baseline) {
					return nil, ErrInvalidTrainingExample
				}
			}
			var gains []float64
			var saturation float64
			if json.Unmarshal(global["gains_rgb"], &gains) != nil ||
				len(gains) != 3 ||
				json.Unmarshal(global["saturation"], &saturation) != nil ||
				math.IsNaN(saturation) || math.IsInf(saturation, 0) {
				return nil, ErrInvalidTrainingExample
			}
			var ratios [3]float64
			for j := range ratios {
				if gains[j] <= 0 || math.IsNaN(gains[j]) || math.IsInf(gains[j], 0) {
					return nil, ErrInvalidTrainingExample
				}
				ratios[j] = math.Log(gains[j] / style.GainsRGB[j])
			}
			// Use a neutral common exposure gauge; residuals retain WB.
			common := (ratios[0] + ratios[1] + ratios[2]) / 3
			target.EV = common / math.Ln2
			target.LogR = ratios[0] - common
			target.LogG = ratios[1] - common
			target.LogB = ratios[2] - common
			target.SaturationDelta = saturation - style.Saturation
		}
		if beautyBody, ok := doc.Modules["beauty"]; ok {
			smoothing, err := scalarBeautySmoothing(beautyBody,
				base.Beauty, style.BeautyEnabled)
			if err != nil {
				return nil, err
			}
			target.RetouchDelta = smoothing - style.Smoothing
		}
		for name := range doc.Modules {
			if name != "global" && name != "beauty" {
				return nil, ErrInvalidTrainingExample
			}
		}
		step := v2draft.CorrectionVector{EV: target.EV - current.EV,
			LogR:            target.LogR - current.LogR,
			LogG:            target.LogG - current.LogG,
			LogB:            target.LogB - current.LogB,
			SaturationDelta: target.SaturationDelta - current.SaturationDelta,
			RetouchDelta:    target.RetouchDelta - current.RetouchDelta}
		current, err = v2draft.SumCorrectionVectors(current, step)
		if err != nil || v2draft.CheckStyleBounds(style, current) != nil {
			return nil, ErrInvalidTrainingExample
		}
		result = append(result, v2draft.ResolvedCorrection{Ref: ref, Vector: step})
	}
	return result, nil
}

func scalarBeautySmoothing(body json.RawMessage,
	baseline map[string]json.RawMessage, enabled bool) (float64, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil {
		return 0, ErrInvalidTrainingExample
	}
	if !enabled {
		// The frozen renderer enables disabled beauty with exactly these
		// non-smoothing defaults for a positive scalar retouch.
		defaults := map[string]any{"enabled": true,
			"algorithm": "beautyfilter_v1", "tone_evenness": 0,
			"texture_keep": 1, "skin_mask": "face68_conservative_v1",
			"protect_features": true}
		baseline = make(map[string]json.RawMessage, len(defaults)+1)
		for name, value := range defaults {
			encoded, err := json.Marshal(value)
			if err != nil {
				return 0, ErrInvalidTrainingDraft
			}
			baseline[name] = encoded
		}
		baseline["smoothing"] = json.RawMessage("0")
	}
	if len(fields) != len(baseline) {
		return 0, ErrInvalidTrainingExample
	}
	for name, value := range baseline {
		if name != "smoothing" && !bytes.Equal(fields[name], value) {
			return 0, ErrInvalidTrainingExample
		}
	}
	var smoothing float64
	if json.Unmarshal(fields["smoothing"], &smoothing) != nil ||
		math.IsNaN(smoothing) || math.IsInf(smoothing, 0) ||
		smoothing < 0 || smoothing > 1 || (!enabled && smoothing == 0) {
		return 0, ErrInvalidTrainingExample
	}
	return smoothing, nil
}
