package v2learning

import "encoding/json"

// Avoid reserving an Evaluate task for effects the installed holdout renderer
// cannot replay completely. This is not a substitute for worker validation or
// qualification, and must be extended when each effect path is installed.
func validateFrozenHoldoutPipeline(raw []byte, reviewedEffects ...bool) error {
	type module struct {
		Enabled bool `json:"enabled"`
	}
	type seriesModule struct {
		Enabled       bool      `json:"enabled"`
		Algorithm     string    `json:"algorithm"`
		Reference     string    `json:"reference"`
		EVLimit       float64   `json:"ev_limit"`
		RGBGainLimits []float64 `json:"rgb_gain_limits"`
	}
	var style struct {
		Defects    module `json:"defects"`
		Geometry   module `json:"geometry"`
		Extensions struct {
			Culling        module       `json:"culling"`
			Series         seriesModule `json:"series"`
			Composition    module       `json:"composition"`
			PortraitDetail module       `json:"portrait_detail"`
			Inpainting     module       `json:"inpainting"`
			Generation     module       `json:"generation"`
			Restoration    *struct {
				Upscale    string `json:"upscale"`
				SensorDust string `json:"sensor_dust"`
				Denoise    string `json:"denoise"`
			} `json:"restoration"`
			Background *struct {
				Mode string `json:"mode"`
			} `json:"background"`
		} `json:"extensions"`
	}
	if json.Unmarshal(raw, &style) != nil {
		return ErrInvalidTrainingDraft
	}
	ext := style.Extensions
	if len(reviewedEffects) > 4 {
		return ErrInvalidTrainingDraft
	}
	defect := len(reviewedEffects) > 0 && reviewedEffects[0]
	dust := len(reviewedEffects) > 1 && reviewedEffects[1]
	detail := len(reviewedEffects) > 2 && reviewedEffects[2]
	series := len(reviewedEffects) > 3 && reviewedEffects[3]
	if (defect && dust) || (defect && detail) || (dust && detail) ||
		(series && (defect || dust || detail)) {
		return ErrInvalidTrainingDraft
	}
	if defect && !style.Defects.Enabled {
		return ErrInvalidTrainingDraft
	}
	if style.Defects.Enabled && !defect {
		return ErrInvalidTrainingDraft
	}
	if ext.PortraitDetail.Enabled != detail {
		return ErrInvalidTrainingDraft
	}
	if ext.Series.Enabled != series {
		return ErrInvalidTrainingDraft
	}
	if series && (ext.Series.Algorithm != "seriesmatch_v1" ||
		ext.Series.Reference != "median" || ext.Series.EVLimit != .35 ||
		len(ext.Series.RGBGainLimits) != 2 ||
		ext.Series.RGBGainLimits[0] != .95 ||
		ext.Series.RGBGainLimits[1] != 1.05) {
		return ErrInvalidTrainingDraft
	}
	if style.Geometry.Enabled || ext.Culling.Enabled || ext.Composition.Enabled || ext.Inpainting.Enabled || ext.Generation.Enabled {
		return ErrInvalidTrainingDraft
	}
	if style.Defects.Enabled && ext.Restoration != nil {
		return ErrInvalidTrainingDraft
	}
	if dust {
		if ext.Restoration == nil || ext.Restoration.Upscale != "off" ||
			ext.Restoration.SensorDust != "review" || ext.Restoration.Denoise != "off" {
			return ErrInvalidTrainingDraft
		}
	} else if ext.Restoration != nil &&
		(ext.Restoration.Upscale != "off" || ext.Restoration.SensorDust != "off") {
		return ErrInvalidTrainingDraft
	}
	if ext.Background != nil && ext.Background.Mode == "generate" {
		return ErrInvalidTrainingDraft
	}
	return nil
}
