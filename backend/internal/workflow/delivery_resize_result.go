package workflow

import (
	"bytes"
	"encoding/json"
	"math"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
)

// deliveryResizeGeometry reproduces the worker's deterministic canvas/chain.
func deliveryResizeGeometry(node CompiledDownloadNode, source review.WorkingImage) (review.Canvas, json.RawMessage, error) {
	var p struct {
		Image        review.ArtifactRef `json:"image"`
		LongEdge     *int64             `json:"long_edge"`
		Width        *int64             `json:"width"`
		Height       *int64             `json:"height"`
		Fit          string             `json:"fit"`
		AllowUpscale bool               `json:"allow_upscale"`
	}
	fail := func() (review.Canvas, json.RawMessage, error) {
		return review.Canvas{}, nil, domain.NewError("DELIVERY_DERIVATIVE_MISMATCH", 409)
	}
	if len(node.Inputs) < 1 || json.Unmarshal(node.Params, &p) != nil || p.Image != node.Inputs[0] || p.Fit != "contain" || (p.LongEdge != nil) == (p.Width != nil || p.Height != nil) || (p.Width == nil) != (p.Height == nil) {
		return fail()
	}
	for _, n := range []*int64{p.LongEdge, p.Width, p.Height} {
		if n != nil && (*n < 1 || *n > 100000) {
			return fail()
		}
	}
	w, h := source.Canvas.Width, source.Canvas.Height
	var scale float64
	if p.LongEdge != nil {
		scale = float64(*p.LongEdge) / float64(max(w, h))
	} else {
		scale = math.Min(float64(*p.Width)/float64(w), float64(*p.Height)/float64(h))
	}
	if !p.AllowUpscale {
		scale = math.Min(1, scale)
	}
	width, height := max(int64(1), int64(math.RoundToEven(float64(w)*scale))), max(int64(1), int64(math.RoundToEven(float64(h)*scale)))
	if width*height > 200000000 {
		return fail()
	}
	name, err := canonical.Hash(map[string]any{"image": p.Image, "width": width, "height": height})
	if err != nil {
		return fail()
	}
	canvas := review.Canvas{Width: width, Height: height, Orientation: 1, SpaceID: "resize:" + name[:32], SourceSHA256: source.Canvas.SourceSHA256}
	var chain struct {
		Steps []json.RawMessage `json:"steps"`
		Hash  string            `json:"hash"`
	}
	if json.Unmarshal(source.TransformChain, &chain) != nil {
		return fail()
	}
	step, err := canonical.Marshal(map[string]any{"kind": "resize", "from_canvas": source.Canvas, "to_canvas": canvas, "invertibility": "invertible", "valid_region": map[string]int64{"x": 0, "y": 0, "width": width, "height": height}, "pixel_centers": "half_pixel"})
	if err != nil {
		return fail()
	}
	chain.Steps = append(chain.Steps, step)
	chain.Hash, err = canonical.Hash(map[string]any{"steps": chain.Steps})
	if err != nil {
		return fail()
	}
	raw, err := canonical.Marshal(chain)
	return canvas, raw, err
}

// ValidateDeliveryResizeResult validates metadata only. Adoption must separately
// prove output producer/fence, physical arrays, retention and current ACL.
func ValidateDeliveryResizeResult(node CompiledDownloadNode, sourceRaw []byte, outputRef review.ArtifactRef, outputRaw []byte, reportRef review.ArtifactRef, reportRaw []byte) (review.WorkingImage, error) {
	fail := func() (review.WorkingImage, error) {
		return review.WorkingImage{}, domain.NewError("DELIVERY_DERIVATIVE_MISMATCH", 409)
	}
	if node.Operation != "derivative.resize" || node.Version != "1" || node.ResourceClass != "cpu" || len(node.Inputs) < 2 || !validExpansionRef(outputRef) || !validExpansionRef(reportRef) || outputRef.ID == reportRef.ID || len(sourceRaw) > 1<<20 || len(outputRaw) > 1<<20 || len(reportRaw) > 1<<20 || renderDerivativeBytesSHA(sourceRaw) != node.Inputs[0].SHA256 || renderDerivativeBytesSHA(outputRaw) != outputRef.SHA256 || renderDerivativeBytesSHA(reportRaw) != reportRef.SHA256 {
		return fail()
	}
	for _, input := range node.Inputs {
		if input.ID == outputRef.ID || input.ID == reportRef.ID {
			return fail()
		}
	}
	source, err := review.ParseWorkingImage(sourceRaw)
	if err != nil {
		return fail()
	}
	output, err := review.ParseWorkingImage(outputRaw)
	if err != nil {
		return fail()
	}
	expected := 2 + boolInt(source.Alpha != nil)
	if len(node.Inputs) != expected || node.Inputs[1] != source.Pixels || (source.Alpha != nil && node.Inputs[2] != *source.Alpha) {
		return fail()
	}
	canvas, chain, err := deliveryResizeGeometry(node, source)
	if err != nil {
		return fail()
	}
	exact, err := canonical.Marshal(output)
	if err != nil || !bytes.Equal(exact, outputRaw) || output.Canvas != canvas || !bytes.Equal(output.TransformChain, chain) || output.Color != source.Color || output.DType != "float32" || source.DType != "float32" || (output.Alpha == nil) != (source.Alpha == nil) {
		return fail()
	}
	same := source.Canvas.Width == canvas.Width && source.Canvas.Height == canvas.Height
	warnings := []string{}
	if same {
		warnings = []string{"no_resize_required"}
	}
	report := DeliveryDerivativeReport{Image: node.Inputs[0], Operation: "resize", Applied: !same, Width: canvas.Width, Height: canvas.Height, MaxChannelDelta: 0, Warnings: warnings}
	exact, err = canonical.Marshal(report)
	if err != nil || !bytes.Equal(exact, reportRaw) {
		return fail()
	}
	return output, nil
}
