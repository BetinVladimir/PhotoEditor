package v2learning

import "github.com/google/uuid"

type ExcludedTrainingCandidate struct {
	EventID uuid.UUID `json:"event_id"`
	Reason  string    `json:"reason"`
}

// PreselectTrainingCandidates applies relational exclusion reasons before
// artifact resolution. It does not imply image/renderer/feature eligibility;
// those checks run during immutable RenderCase compilation.
func PreselectTrainingCandidates(rows []TrainingCandidate) (
	[]TrainingCandidate, []ExcludedTrainingCandidate) {
	eligible := make([]TrainingCandidate, 0, len(rows))
	excluded := make([]ExcludedTrainingCandidate, 0)
	for _, row := range rows {
		reason := ""
		switch {
		case !row.ConsentLive:
			reason = "CONSENT_REVOKED"
		case !row.SourceReady:
			reason = "SOURCE_UNAVAILABLE"
		case !row.BaseReady:
			reason = "BASE_UNAVAILABLE"
		case row.ApprovalID == nil || !row.ApprovedReady:
			reason = "UNAPPROVED_RESULT"
		default:
			for _, path := range row.ChangedPaths {
				if path == "/crop" || path == "/selection" {
					reason = "NONPARAMETRIC_EDIT"
					break
				}
			}
		}
		if reason != "" {
			excluded = append(excluded, ExcludedTrainingCandidate{
				EventID: row.EventID, Reason: reason})
		} else {
			eligible = append(eligible, row)
		}
	}
	return eligible, excluded
}
