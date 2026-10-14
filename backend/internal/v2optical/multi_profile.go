package v2optical

import (
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

func CompileMultiExecutionProfile(operation string, paramsRaw []byte, originals [][]byte,
	qualified VerifiedMultiQualification, capacity MultiResourceEstimate) (json.RawMessage, error) {
	pin, err := qualified.Pin()
	if err != nil {
		return nil, err
	}
	if qualified.operation != operation || qualified.paramsRaw != string(paramsRaw) || len(qualified.sourceRaw) != len(originals) {
		return nil, v2input.ErrInvalid
	}
	for i, raw := range originals {
		if qualified.sourceRaw[i] != string(raw) {
			return nil, v2input.ErrInvalid
		}
	}
	required, err := MultiResources(operation, paramsRaw, originals)
	if err != nil {
		return nil, err
	}
	if err := ValidateMultiCapacity(required, capacity); err != nil {
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
		Backend       string                `json:"backend"`
		Renderer      string                `json:"renderer_version"`
		RuntimeSHA    string                `json:"multi_runtime_sha"`
		Qualification MultiQualificationPin `json:"multi_qualification"`
		Resources     MultiResourceEstimate `json:"resource_estimate"`
	}{"cpu", "v2.2", pin.RuntimeLockSHA, pin, required})
}
