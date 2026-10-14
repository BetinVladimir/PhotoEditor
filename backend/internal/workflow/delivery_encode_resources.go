package workflow

import (
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
)

// CompileDeliveryEncodeProfile matches export.memory.presentation_ram_bytes.
// Source refers to the last validated presentation output, not the original
// image when derivatives ran. Reserves downloaded NPY and encoded output bytes.
func CompileDeliveryEncodeProfile(node CompiledDownloadNode, sourceRaw []byte, limits DeliveryDerivativeLimits) (json.RawMessage, error) {
	fail := func() (json.RawMessage, error) { return nil, domain.NewError("DELIVERY_RESOURCE_INVALID", 422) }
	if node.Operation != "export.encode" || node.Version != "1" || node.ResourceClass != "cpu" || len(node.Inputs) < 2 || len(sourceRaw) > 1<<20 || renderDerivativeBytesSHA(sourceRaw) != node.Inputs[0].SHA256 {
		return fail()
	}
	image, err := review.ParseWorkingImage(sourceRaw)
	if err != nil {
		return fail()
	}
	if image.DType != "float32" || image.Color.ProfileID != "work_rec2020_gamma24_v1" || node.Inputs[1] != image.Pixels || len(node.Inputs) != 2+boolInt(image.Alpha != nil) {
		return fail()
	}
	if image.Alpha != nil && node.Inputs[2] != *image.Alpha {
		return fail()
	}
	var p encodeParams
	if json.Unmarshal(node.Params, &p) != nil || p.Image != node.Inputs[0] || domainDigestInvalid(p.ICCSHA256) {
		return fail()
	}
	preset := DownloadPreset{Format: p.Format, ICCID: p.ICCID, Size: p.Size, Quality: p.Quality, Subsampling: p.Subsampling, Alpha: p.Alpha, MatteRGB: p.MatteRGB, MetadataPolicy: p.MetadataPolicy}
	if !validDownloadPreset(preset) {
		return fail()
	}
	w, h := image.Canvas.Width, image.Canvas.Height
	width, height := encodeDimensions(w, h, p.Size)

	source, target := w*h, width*height
	if source < 1 || source > 200000000 || target < 1 || target > 200000000 {
		return fail()
	}
	sourceBytes := int64(12)
	if image.Alpha != nil {
		sourceBytes = 16
	}
	outputChannels := int64(6)
	if image.Alpha != nil && p.Alpha == "preserve" {
		outputChannels = 8
	}
	output := (8 << 20) + target*outputChannels
	estimate := ResourceEstimate{RAMBytes: encodeRAMBytes(source, target, image.Alpha != nil), VRAMBytes: 0, ScratchBytes: (8 << 20) + source*sourceBytes + output, OutputBytes: output, PixelCount: target}
	if limits.RAMBytes < estimate.RAMBytes || limits.ScratchBytes < estimate.ScratchBytes || limits.OutputBytes < estimate.OutputBytes {
		return fail()
	}
	return canonical.Marshal(map[string]any{"backend": "cpu", "renderer_version": "v2.2", "resource_estimate": estimate})
}
