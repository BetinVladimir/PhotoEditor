package v2draft

import (
	"bytes"
	"encoding/json"
	"math"
	"photoeditor/backend/internal/v2input"
)

// upscaleReportRefs validates the generated UpscaleReport contract before
// traversing its source. Current artifact authority remains the catalog's job.
func upscaleReportRefs(raw []byte, path string, parent v2input.Ref) ([]LayerRef, error) {
	f, ok := exactFields(raw, "image", "model_id", "model_lock_id", "backend", "scale", "color_adapter_id", "tile", "overlap", "input_pixel_count", "output_pixel_count", "disabled_delta_fraction", "source_clip_fraction", "gamut_excess_fraction", "alpha_resampled", "warnings")
	bad := func() ([]LayerRef, error) { return nil, v2input.ErrInvalid }
	if !ok || !isDocumentRef(parent) {
		return bad()
	}
	for _, value := range f {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return bad()
		}
	}
	image, valid := layerRef(f["image"])
	if !valid {
		return bad()
	}
	var model, lock, backend, adapter string
	if json.Unmarshal(f["model_id"], &model) != nil || (model != "realesrgan_x2" && model != "realesrgan_x4") || json.Unmarshal(f["model_lock_id"], &lock) != nil || !correctionDigest(lock) || json.Unmarshal(f["backend"], &backend) != nil || (backend != "cpu" && backend != "cuda" && backend != "mps") || json.Unmarshal(f["color_adapter_id"], &adapter) != nil || adapter != "work_srgb_delta_v1" {
		return bad()
	}
	var scale, tile, overlap int
	if json.Unmarshal(f["scale"], &scale) != nil || (scale != 2 && scale != 4) || json.Unmarshal(f["tile"], &tile) != nil || tile != 256 || json.Unmarshal(f["overlap"], &overlap) != nil || overlap != 32 {
		return bad()
	}
	for _, name := range []string{"input_pixel_count", "output_pixel_count"} {
		var count int64
		if json.Unmarshal(f[name], &count) != nil || count < 1 || count > 200000000 {
			return bad()
		}
	}
	for _, name := range []string{"disabled_delta_fraction", "source_clip_fraction", "gamut_excess_fraction"} {
		var value float64
		if json.Unmarshal(f[name], &value) != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
			return bad()
		}
	}
	var alpha bool
	var warnings []string
	if json.Unmarshal(f["alpha_resampled"], &alpha) != nil || json.Unmarshal(f["warnings"], &warnings) != nil || warnings == nil || len(warnings) > 2 {
		return bad()
	}
	for _, warning := range warnings {
		if warning != "source_srgb_clipped" && warning != "delta_gamut_excess" {
			return bad()
		}
	}
	return []LayerRef{{Path: path + ".image", Contract: "WorkingImage", Ref: image, ParentRef: parent}}, nil
}
