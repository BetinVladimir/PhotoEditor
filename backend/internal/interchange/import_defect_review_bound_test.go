package interchange

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type defectReviewPreparedReader struct{ raw []byte }

func (r defectReviewPreparedReader) ReadPreparedImportDocument(_ context.Context, entry v2input.Entry, _ int64) ([]byte, error) {
	if entry.Contract != "DefectCandidates" || entry.Ref.SHA256 != rawSHA(r.raw) {
		return nil, v2input.ErrInvalid
	}
	return append([]byte(nil), r.raw...), nil
}

type defectReviewCandidateQualifier struct{}

func (defectReviewCandidateQualifier) ValidateArchivedImportDefectCandidates(context.Context, []byte, v2input.SessionVersionBinding) error {
	return nil
}
func (defectReviewCandidateQualifier) QualifyImportedDefectCandidates(context.Context, []byte, v2input.SessionVersionBinding) error {
	return nil
}

type defectReviewIssuer struct{ forged bool }

func (i defectReviewIssuer) ReissueImportedDefectReview(_ context.Context, _, current review.DefectSelection, _, _ v2input.SessionVersionBinding) (review.DefectSelection, error) {
	if i.forged {
		current.Accepted = []string{rawSHA([]byte("unknown candidate"))}
	}
	return current, nil
}
func (defectReviewIssuer) QualifyPreparedDefectReview(context.Context, review.DefectSelection, v2input.SessionVersionBinding) error {
	return nil
}

func TestBoundImportDefectReviewRejectsUnknownIssuedCandidate(t *testing.T) {
	proposalRaw, err := os.ReadFile("../review/testdata/defect-proposal-python.json")
	if err != nil {
		t.Fatal(err)
	}
	selectionRaw, err := os.ReadFile("../review/testdata/defect-selection-python.json")
	if err != nil {
		t.Fatal(err)
	}
	selection, err := review.ParseDefectSelection(selectionRaw, review.ArtifactRef{ID: uuid.New(), SHA256: rawSHA(selectionRaw)})
	if err != nil {
		t.Fatal(err)
	}
	target := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: selection.Image.ID, SHA256: selection.Image.SHA256}, CanvasHash: rawSHA([]byte("canvas"))}
	authority := BoundImportDefectReviewAuthority{Documents: defectReviewPreparedReader{raw: proposalRaw}, Candidates: defectReviewCandidateQualifier{}, Current: defectReviewIssuer{}, Target: target}
	if _, err := authority.ReissueImportedDefectReview(context.Background(), selection, selection, target, target); err != nil {
		t.Fatal("known candidate was rejected", err)
	}
	authority.Current = defectReviewIssuer{forged: true}
	if _, err := authority.ReissueImportedDefectReview(context.Background(), selection, selection, target, target); err == nil {
		t.Fatal("unknown issued candidate was accepted")
	}
}
