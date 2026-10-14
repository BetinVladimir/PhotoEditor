package workflow

import (
	"encoding/json"
	"math"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
)

type DeliveryDerivativeLimits struct{ RAMBytes, ScratchBytes, OutputBytes int64 }

// CompileDeliveryDerivativeProfile derives reservations from hash-verified
// source dimensions and exact compiled params. Conservative frame-buffer policy;
// qualification must measure peaks before claiming hardware capacity/SLA.
func CompileDeliveryDerivativeProfile(node CompiledDownloadNode, sourceRaw []byte, limits DeliveryDerivativeLimits) (json.RawMessage, error) {
	fail := func() (json.RawMessage, error) { return nil, domain.NewError("DELIVERY_RESOURCE_INVALID", 422) }
	if node.Version != "1" || node.ResourceClass != "cpu" || len(node.Inputs) < 2 || len(sourceRaw) > 1<<20 || renderDerivativeBytesSHA(sourceRaw) != node.Inputs[0].SHA256 {
		return fail()
	}
	image, err := review.ParseWorkingImage(sourceRaw)
	if err != nil {
		return fail()
	}
	if image.DType != "float32" || image.Color.ProfileID != "work_rec2020_gamma24_v1" || node.Inputs[1] != image.Pixels {
		return fail()
	}
	expected := 2
	if image.Alpha != nil {
		expected++
		if len(node.Inputs) != expected || node.Inputs[2] != *image.Alpha {
			return fail()
		}
	}
	if len(node.Inputs) != expected {
		return fail()
	}
	w, h := image.Canvas.Width, image.Canvas.Height
	width, height := w, h
	var params struct {
		Image        review.ArtifactRef `json:"image"`
		LongEdge     *int64             `json:"long_edge"`
		Width        *int64             `json:"width"`
		Height       *int64             `json:"height"`
		Fit          string             `json:"fit"`
		AllowUpscale *bool               `json:"allow_upscale"`
	}
	if json.Unmarshal(node.Params, &params) != nil || params.Image != node.Inputs[0] {
		return fail()
	}
	switch node.Operation {
	case "derivative.resize":
		long := params.LongEdge != nil
		box := params.Width != nil || params.Height != nil
		if long == box || (params.Width == nil) != (params.Height == nil) || params.Fit != "contain" || params.AllowUpscale == nil {
			return fail()
		}
		for _, n := range []*int64{params.LongEdge, params.Width, params.Height} {
			if n != nil && (*n < 1 || *n > 100000) {
				return fail()
			}
		}
		var scale float64
		if long {
			scale = float64(*params.LongEdge) / float64(max(w, h))
		} else {
			scale = math.Min(float64(*params.Width)/float64(w), float64(*params.Height)/float64(h))
		}
		if !*params.AllowUpscale {
			scale = math.Min(1, scale)
		}
		width = max(int64(1), int64(math.RoundToEven(float64(w)*scale)))
		height = max(int64(1), int64(math.RoundToEven(float64(h)*scale)))
	case "derivative.sharpen":
	default:
		return fail()
	}
	source, target := w*h, width*height
	if source < 1 || source > 200000000 || target < 1 || target > 200000000 {
		return fail()
	}
	channels := int64(12)
	if image.Alpha != nil {
		channels = 16
	}
	estimate := ResourceEstimate{RAMBytes: (256 << 20) + 192*(source+target), VRAMBytes: 0, ScratchBytes: (32 << 20) + channels*(source+target), OutputBytes: (8 << 20) + channels*target, PixelCount: target}
	if limits.RAMBytes < estimate.RAMBytes || limits.ScratchBytes < estimate.ScratchBytes || limits.OutputBytes < estimate.OutputBytes {
		return fail()
	}
	return canonical.Marshal(map[string]any{"backend": "cpu", "renderer_version": "v2.2", "resource_estimate": estimate})
}
