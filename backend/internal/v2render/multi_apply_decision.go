package v2render

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// A decision is not authority. The commit must repeat all bindings under locks,
// compare collection revision and retain the original version and its edits.
// The new geometry version starts without source-coordinate masks/layers.
type MultiApplyDecision struct {
	SchemaVersion         int         `json:"schema_version"`
	ResultID              uuid.UUID   `json:"result_id"`
	Root                  v2input.Ref `json:"root"`
	QualityRun            uuid.UUID   `json:"quality_run_id"`
	QualityResult         uuid.UUID   `json:"quality_result_id"`
	QualityReport         v2input.Ref `json:"quality_report"`
	ConfirmGeometryReset  bool        `json:"confirm_geometry_reset"`
	AcceptQualityWarnings bool        `json:"accept_quality_warnings"`
}

func ValidateMultiApplyDecision(decision MultiApplyDecision, current MultiQualityReview) error {
	if err := validateMultiQualityReview(current); err != nil {
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
