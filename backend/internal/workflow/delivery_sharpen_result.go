package workflow

import (
	"bytes"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
)

type DeliveryDerivativeReport struct {
	Image           review.ArtifactRef `json:"image"`
	Operation       string             `json:"operation"`
	Applied         bool               `json:"applied"`
	Width           int64              `json:"width"`
	Height          int64              `json:"height"`
	MaxChannelDelta float64            `json:"max_channel_delta"`
	Warnings        []string           `json:"warnings"`
}

// ValidateDeliverySharpenResult checks exact document bytes and preserved image
// metadata. Caller must prove task/fence/output provenance and inspect physical
// pixels/alpha under current ACL; JSON is not evidence of photographic quality.
func ValidateDeliverySharpenResult(node CompiledDownloadNode, sourceRaw []byte, outputRef review.ArtifactRef, outputRaw []byte, reportRef review.ArtifactRef, reportRaw []byte) (review.WorkingImage, error) {
	fail := func() (review.WorkingImage, error) {
		return review.WorkingImage{}, domain.NewError("DELIVERY_DERIVATIVE_MISMATCH", 409)
	}
	if node.Operation != "derivative.sharpen" || node.Version != "1" || node.ResourceClass != "cpu" || len(node.Inputs) < 2 ||
		!validExpansionRef(outputRef) || !validExpansionRef(reportRef) || outputRef.ID == reportRef.ID ||
		len(sourceRaw) > 1<<20 || len(outputRaw) > 1<<20 || len(reportRaw) > 1<<20 ||
		renderDerivativeBytesSHA(sourceRaw) != node.Inputs[0].SHA256 || renderDerivativeBytesSHA(outputRaw) != outputRef.SHA256 || renderDerivativeBytesSHA(reportRaw) != reportRef.SHA256 {
		return fail()
	}
	for _, input := range node.Inputs {
		if input.ID == outputRef.ID || input.ID == reportRef.ID {
			return fail()
		}
	}
	var params struct {
		Image    review.ArtifactRef `json:"image"`
		Strength float64            `json:"strength"`
		MaxDelta float64            `json:"max_delta"`
	}
	if json.Unmarshal(node.Params, &params) != nil || params.Image != node.Inputs[0] || params.Strength < 0 || params.Strength > 1 || params.MaxDelta <= 0 || params.MaxDelta > .03 {
		return fail()
	}
	source, err := review.ParseWorkingImage(sourceRaw)
	if err != nil {
		return fail()
	}
	output, err := review.ParseWorkingImage(outputRaw)
	if err != nil {
		return fail()
	}
	exact, err := canonical.Marshal(output)
	if err != nil || !bytes.Equal(exact, outputRaw) || output.Canvas != source.Canvas || output.Color != source.Color || output.DType != source.DType || output.DType != "float32" ||
		!bytes.Equal(output.TransformChain, source.TransformChain) || (output.Alpha == nil) != (source.Alpha == nil) {
		return fail()
	}
	if node.Inputs[1] != source.Pixels || len(node.Inputs) != 2+boolInt(source.Alpha != nil) {
		return fail()
	}
	if source.Alpha != nil && node.Inputs[2] != *source.Alpha {
		return fail()
	}
	var report DeliveryDerivativeReport
	if json.Unmarshal(reportRaw, &report) != nil {
		return fail()
	}
	exact, err = canonical.Marshal(report)
	if err != nil || !bytes.Equal(exact, reportRaw) || report.Image != params.Image || report.Operation != "sharpen" || report.Width != output.Canvas.Width || report.Height != output.Canvas.Height || report.Applied != (params.Strength > 0) || report.MaxChannelDelta < 0 || report.MaxChannelDelta > params.MaxDelta+1e-7 {
		return fail()
	}
	if params.Strength == 0 {
		if report.MaxChannelDelta != 0 || len(report.Warnings) != 1 || report.Warnings[0] != "zero_strength" {
			return fail()
		}
	} else if len(report.Warnings) != 0 {
		return fail()
	}
	return output, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
