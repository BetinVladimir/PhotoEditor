package v2draft

import (
	"encoding/json"
	"math"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// BuildAnalysisOverride compiles one source-bound V22 scalar correction into
// the existing OverrideV1 renderer contract. The entire GlobalColor module is
// copied from the accepted StyleV2, changing only gains and saturation;
// beauty keeps its non-smoothing fields when already enabled.
func BuildAnalysisOverride(styleRaw []byte, sourceSHA string,
	delta CorrectionVector) ([]byte, error) {
	base, err := DecodeStyleBaseline(styleRaw)
	if err != nil || !correctionDigest(sourceSHA) || CheckStyleBounds(base, delta) != nil {
		return nil, v2input.ErrInvalid
	}
	var style map[string]json.RawMessage
	if json.Unmarshal(styleRaw, &style) != nil {
		return nil, v2input.ErrInvalid
	}
	var global map[string]json.RawMessage
	if json.Unmarshal(style["global"], &global) != nil || len(global) == 0 {
		return nil, v2input.ErrInvalid
	}
	gains := [3]float64{}
	logs := [3]float64{delta.LogR, delta.LogG, delta.LogB}
	for i := range gains {
		gains[i] = base.GainsRGB[i] * math.Pow(2, delta.EV) * math.Exp(logs[i])
	}
	global["gains_rgb"], err = json.Marshal(gains)
	if err != nil {
		return nil, err
	}
	global["saturation"], err = json.Marshal(base.Saturation + delta.SaturationDelta)
	if err != nil {
		return nil, err
	}
	modules := map[string]any{"global": global}
	if delta.RetouchDelta != 0 {
		var beauty map[string]json.RawMessage
		if base.BeautyEnabled {
			if json.Unmarshal(style["beauty"], &beauty) != nil || len(beauty) == 0 {
				return nil, v2input.ErrInvalid
			}
		} else {
			beauty = map[string]json.RawMessage{}
			for name, value := range map[string]any{
				"enabled": true, "algorithm": "beautyfilter_v1",
				"tone_evenness": 0, "texture_keep": 1,
				"skin_mask": "face68_conservative_v1", "protect_features": true,
			} {
				beauty[name], err = json.Marshal(value)
				if err != nil {
					return nil, err
				}
			}
		}
		beauty["smoothing"], err = json.Marshal(base.Smoothing + delta.RetouchDelta)
		if err != nil {
			return nil, err
		}
		modules["beauty"] = beauty
	}
	return canonical.Marshal(struct {
		SchemaVersion int            `json:"schema_version"`
		InputSHA256   string         `json:"input_sha256"`
		Modules       map[string]any `json:"modules"`
		Revision      int            `json:"revision"`
	}{1, sourceSHA, modules, 1})
}
