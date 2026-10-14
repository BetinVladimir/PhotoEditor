package semanticproposal

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestNativeFaceProducerReportsRequireOriginalSelectedReportObjects(t *testing.T) {
	reports := nativeFaceReportProof{}
	for i := range reports.reports {
		reports.reports[i] = personEvidenceObject{ref: v2input.Ref{ArtifactID: uuid.New()}}
	}
	reports.originalReports = reports.reports
	faces := []facePreparationProof{{qualification: [2]personEvidenceObject{reports.reports[0], reports.reports[1]}}}
	parsers := []faceParsePreparationProof{{evidence: facePreparationProof{qualification: [2]personEvidenceObject{reports.reports[2], reports.reports[1]}}}}
	if err := matchNativeFaceProducerReports(reports, faces, parsers); err != nil {
		t.Fatal(err)
	}
	if err := matchNativeFaceProducerReports(reports, faces, nil); err != nil {
		t.Fatal("manual-only declaration", err)
	}
	for _, mode := range []string{"landmarks", "landmark-detector", "parser", "parser-detector", "original-report-set"} {
		changedReports := reports
		changedFaces := append([]facePreparationProof(nil), faces...)
		changedParsers := append([]faceParsePreparationProof(nil), parsers...)
		switch mode {
		case "landmarks":
			changedFaces[0].qualification[0].ref.ArtifactID = uuid.New()
		case "landmark-detector":
			changedFaces[0].qualification[1].ref.ArtifactID = uuid.New()
		case "parser":
			changedParsers[0].evidence.qualification[0].ref.ArtifactID = uuid.New()
		case "parser-detector":
			changedParsers[0].evidence.qualification[1].ref.ArtifactID = uuid.New()
		case "original-report-set":
			changedReports.reports[2].ref.ArtifactID = uuid.New()
		}
		if err := matchNativeFaceProducerReports(changedReports, changedFaces, changedParsers); err == nil {
			t.Fatal("substituted producer report accepted", mode)
		}
	}
}
