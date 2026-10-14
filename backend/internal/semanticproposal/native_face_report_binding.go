package semanticproposal

import "photoeditor/backend/internal/v2input"

// Each original producer proof must retain the same deployment-selected report
// objects as the independently retained complete report set. Matching policy
// locks alone do not authorize substitution of an alternative report artifact.
func matchNativeFaceProducerReports(reports nativeFaceReportProof, faces []facePreparationProof, parsers []faceParsePreparationProof) error {
	if reports.reports != reports.originalReports {
		return v2input.ErrConflict
	}
	for _, face := range faces {
		if face.qualification != [2]personEvidenceObject{reports.reports[0], reports.reports[1]} {
			return v2input.ErrConflict
		}
	}
	for _, parser := range parsers {
		if parser.evidence.qualification != [2]personEvidenceObject{reports.reports[2], reports.reports[1]} {
			return v2input.ErrConflict
		}
	}
	return nil
}
