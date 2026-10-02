package v2draft

import (
	"encoding/json"
	"math"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type StyleBaseline struct {
	GainsRGB      [3]float64
	Saturation    float64
	Smoothing     float64
	BeautyEnabled bool
}

// DecodeStyleBaseline takes only the scalar fields affected by a correction.
// The source bytes must already be the accepted, hash-verified StyleV2.
func DecodeStyleBaseline(raw []byte) (StyleBaseline, error) {
	var style struct {
		SchemaVersion   string `json:"schema_version"`
		RendererVersion string `json:"renderer_version"`
		Global          struct {
			GainsRGB   []float64 `json:"gains_rgb"`
			Saturation float64   `json:"saturation"`
		} `json:"global"`
		Beauty struct {
			Enabled   bool    `json:"enabled"`
			Smoothing float64 `json:"smoothing"`
		} `json:"beauty"`
	}
	if json.Unmarshal(raw, &style) != nil || style.SchemaVersion != "2.0.0" ||
		style.RendererVersion != "photo_renderer_v2" || len(style.Global.GainsRGB) != 3 {
		return StyleBaseline{}, v2input.ErrInvalid
	}
	base := StyleBaseline{Saturation: style.Global.Saturation,
		Smoothing: style.Beauty.Smoothing, BeautyEnabled: style.Beauty.Enabled}
	copy(base.GainsRGB[:], style.Global.GainsRGB)
	if !base.valid() || (!base.BeautyEnabled && base.Smoothing != 0) {
		return StyleBaseline{}, v2input.ErrInvalid
	}
	return base, nil
}

func inRange(value, low, high float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= low && value <= high
}

func (b StyleBaseline) valid() bool {
	for _, gain := range b.GainsRGB {
		if !inRange(gain, 0.25, 4) {
			return false
		}
	}
	return inRange(b.Saturation, 0, 2) && inRange(b.Smoothing, 0, 1)
}

// CheckStyleBounds applies the exact exposure/log-gain formula before
// approval. Out-of-range style values fail; there is no silent clamp.
func CheckStyleBounds(base StyleBaseline, delta CorrectionVector) error {
	if !base.valid() || !delta.valid() {
		return v2input.ErrInvalid
	}
	logs := [3]float64{delta.LogR, delta.LogG, delta.LogB}
	for i, gain := range base.GainsRGB {
		if !inRange(gain*math.Pow(2, delta.EV)*math.Exp(logs[i]), 0.25, 4) {
			return v2input.ErrInvalid
		}
	}
	if !inRange(base.Saturation+delta.SaturationDelta, 0, 2) ||
		!inRange(base.Smoothing+delta.RetouchDelta, 0, 1) {
		return v2input.ErrInvalid
	}
	return nil
}

type FrozenRecipe struct {
	SchemaVersion   int                           `json:"schema_version"`
	Source          v2input.SessionVersionBinding `json:"source"`
	BaseStyle       v2input.Ref                   `json:"base_style"`
	EffectiveDelta  CorrectionVector              `json:"effective_delta"`
	RendererVersion string                        `json:"renderer_version"`
	ICCLockSHA      string                        `json:"icc_lock_sha"`
	ModelLockSHA    string                        `json:"model_lock_sha"`
}

// BuildFrozenRecipe serializes the effective, pinned recipe only after the
// caller has validated the source, style, session, learning and overrides.
func BuildFrozenRecipe(binding v2input.SessionVersionBinding, style v2input.Ref,
	base StyleBaseline, delta CorrectionVector, iccLock, modelLock string) ([]byte, error) {
	if binding.AssetVersionID == uuid.Nil || !isDocumentRef(binding.Source) ||
		!correctionDigest(binding.CanvasHash) || !isDocumentRef(style) ||
		!correctionDigest(iccLock) || !correctionDigest(modelLock) ||
		CheckStyleBounds(base, delta) != nil {
		return nil, v2input.ErrInvalid
	}
	return canonical.Marshal(FrozenRecipe{SchemaVersion: 1, Source: binding,
		BaseStyle: style, EffectiveDelta: delta, RendererVersion: "v2.2",
		ICCLockSHA: iccLock, ModelLockSHA: modelLock})
}
