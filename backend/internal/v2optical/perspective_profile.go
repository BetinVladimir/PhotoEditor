package v2optical

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// CompilePerspectiveManualExecutionProfile binds qualified bytes to original
// source and effective manual output dimensions. Live SQL authority is separate.
func CompilePerspectiveManualExecutionProfile(params PerspectiveParams, sourceRaw []byte, qualified VerifiedPerspectiveQualification,
	maxPixels int64, capacity PerspectiveResourceEstimate) (json.RawMessage, error) {
	image, err := VerifyPerspectiveWorkingImage(params, sourceRaw)
	if err != nil {
		return nil, err
	}
	bounds, err := CompilePerspectiveManualGeometry(context.Background(), params, image.Canvas.Width, image.Canvas.Height, maxPixels)
	if err != nil {
		return nil, err
	}
	return compilePerspectiveSizedProfile(params, sourceRaw, qualified, bounds.Width, bounds.Height, capacity)
}

func compilePerspectiveSizedProfile(params PerspectiveParams, sourceRaw []byte, qualified VerifiedPerspectiveQualification, outWidth, outHeight int64, capacity PerspectiveResourceEstimate) (json.RawMessage, error) {
	image, err := VerifyPerspectiveWorkingImage(params, sourceRaw)
	if err != nil {
		return nil, err
	}
	pin, err := qualified.Pin()
	if err != nil {
		return nil, err
	}
	body, _, err := params.Compile()
	if err != nil {
		return nil, err
	}
	if string(body) != qualified.paramsRaw || qualified.inputICC != image.Color.ICCSHA256 {
		return nil, v2input.ErrInvalid
	}
	required, err := PerspectiveResources(image.Canvas.Width, image.Canvas.Height, outWidth, outHeight)
	if err != nil {
		return nil, err
	}
	if err := ValidatePerspectiveCapacity(required, capacity); err != nil {
		return nil, err
	}
	expected, err := qualified.locks.Expected()
	if err != nil {
		return nil, err
	}
	report, err := v2qualification.Verify(qualified.reportRaw, pin.Report.SHA256, expected)
	if err != nil {
		return nil, err
	}
	if report.Metrics.PeakVRAMBytes != 0 || report.Metrics.PeakRSSBytes > float64(required.RAMBytes) || report.Metrics.ScratchBytes > float64(required.ScratchBytes) {
		return nil, v2qualification.ErrUnqualified
	}
	return canonical.Marshal(struct {
		Backend       string                      `json:"backend"`
		Renderer      string                      `json:"renderer_version"`
		RuntimeSHA    string                      `json:"perspective_runtime_sha"`
		Qualification PerspectiveQualificationPin `json:"perspective_qualification"`
		Resources     PerspectiveResourceEstimate `json:"resource_estimate"`
	}{"cpu", "v2.2", pin.RuntimeLockSHA, pin, required})
}
