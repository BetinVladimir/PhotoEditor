package interchange

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type liveReviewDecisionStub struct {
	value ImportLiveReviewDecision
	err   error
	calls int
}

func (s *liveReviewDecisionStub) Read(_ context.Context, _ uuid.UUID, _ v2input.SessionVersionBinding, _ string, _, _ v2input.Ref, _ *v2input.Ref) (ImportLiveReviewDecision, error) {
	s.calls++
	return s.value, s.err
}

func TestSQLImportDefectReviewIssuerReplacesArchivedSelection(t *testing.T) {
	base := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("base"))}
	target := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: base, CanvasHash: rawSHA([]byte("canvas"))}
	proposal := review.ArtifactRef{ID: uuid.New(), SHA256: rawSHA([]byte("proposal"))}
	label := review.ArtifactRef{ID: uuid.New(), SHA256: rawSHA([]byte("label"))}
	archivedID, currentID := rawSHA([]byte("archived")), rawSHA([]byte("current"))
	selection := review.DefectSelection{Version: "defect_review_v1", Proposal: proposal, Image: review.ArtifactRef{ID: base.ArtifactID, SHA256: base.SHA256}, LabelMap: label, Revision: 1, Accepted: []string{archivedID}, Rejected: []string{}}
	stub := &liveReviewDecisionStub{value: ImportLiveReviewDecision{Decision: "accept", Accepted: []string{currentID}, Rejected: []string{}, Epoch: 7}}
	authority := SQLImportDefectReviewIssuer{Entry: uuid.New(), Target: target, Decisions: stub}
	issued, err := authority.ReissueImportedDefectReview(context.Background(), selection, selection, target, target)
	if err != nil || issued.Revision != 7 || len(issued.Accepted) != 1 || issued.Accepted[0] != currentID || stub.calls != 1 {
		t.Fatal("archived decision was not replaced", issued, err)
	}
	if err := authority.QualifyPreparedDefectReview(context.Background(), issued, target); err != nil || stub.calls != 3 {
		t.Fatal("issued decision was not rechecked", err)
	}
	stub.value.Epoch = 8
	if err := authority.QualifyPreparedDefectReview(context.Background(), issued, target); err == nil {
		t.Fatal("stale epoch qualified")
	}
	stub.err = ErrImportReviewAwaitingDecision
	if _, err := authority.ReissueImportedDefectReview(context.Background(), selection, selection, target, target); !errors.Is(err, ErrImportReviewAwaitingDecision) {
		t.Fatal("pending decision lost", err)
	}
}

func TestSQLImportDustReviewIssuerRequiresCurrentGrant(t *testing.T) {
	base := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("dust-base"))}
	target := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: base, CanvasHash: rawSHA([]byte("canvas"))}
	selection := review.DustSelection{Version: "sensor_dust_review_v1", Proposal: review.ArtifactRef{ID: uuid.New(), SHA256: rawSHA([]byte("dust-proposal"))}, Base: review.ArtifactRef{ID: base.ArtifactID, SHA256: base.SHA256}, Revision: 1, Accepted: []string{rawSHA([]byte("archived"))}, Rejected: []string{}}
	stub := &liveReviewDecisionStub{value: ImportLiveReviewDecision{Decision: "skip", Accepted: []string{}, Rejected: []string{}, Epoch: 5}}
	authority := SQLImportDustReviewIssuer{Entry: uuid.New(), Target: target, Decisions: stub}
	issued, err := authority.ReissueImportedDustReview(context.Background(), selection, selection, target, target)
	if err != nil || issued.Revision != 5 || issued.Accepted == nil || len(issued.Accepted) != 0 {
		t.Fatal("archived dust selection survived", issued, err)
	}
	if err := authority.QualifyPreparedDustReview(context.Background(), issued, target); err != nil {
		t.Fatal("current dust grant rejected", err)
	}
	stub.err = ErrImportReviewAwaitingDecision
	if err := authority.QualifyPreparedDustReview(context.Background(), issued, target); !errors.Is(err, ErrImportReviewAwaitingDecision) {
		t.Fatal("revoked dust grant accepted", err)
	}
}
