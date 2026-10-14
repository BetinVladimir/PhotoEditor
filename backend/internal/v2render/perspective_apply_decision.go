package v2render

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// A decision is not authority. The commit must repeat all bindings under locks,
// compare collection revision and retain the original version and its edits.
// The new geometry version starts without source-coordinate masks/layers.
type PerspectiveApplyDecision struct {
	SchemaVersion         int         `json:"schema_version"`
	ResultID              uuid.UUID   `json:"result_id"`
	Root                  v2input.Ref `json:"root"`
	QualityRun            uuid.UUID   `json:"quality_run_id"`
	QualityResult         uuid.UUID   `json:"quality_result_id"`
	QualityReport         v2input.Ref `json:"quality_report"`
	ConfirmGeometryReset  bool        `json:"confirm_geometry_reset"`
	AcceptQualityWarnings bool        `json:"accept_quality_warnings"`
}

func ValidatePerspectiveApplyDecision(decision PerspectiveApplyDecision, current PerspectiveQualityReview) error {
	if err := validatePerspectiveQualityReview(current); err != nil {
		return err
	}
	if decision.SchemaVersion != 1 || !decision.ConfirmGeometryReset || decision.ResultID != current.Proposal.ResultID || decision.Root != current.Proposal.Result || decision.QualityRun != current.QualityRun || decision.QualityResult != current.QualityResult || decision.QualityReport != current.ReportRef {
		return v2input.ErrInvalid
	}
	status, err := perspectiveQualityStatus(current.Report)
	if err != nil {
		return err
	}
	if status == "fail" || status == "review" && !decision.AcceptQualityWarnings {
		return v2input.ErrInvalid
	}
	return nil
}

func perspectiveQualityStatus(raw json.RawMessage) (string, error) {
	var report struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(raw, &report) != nil {
		return "", v2input.ErrInvalid
	}
	switch report.Status {
	case "pass", "review", "fail":
		return report.Status, nil
	default:
		return "", v2input.ErrInvalid
	}
}
