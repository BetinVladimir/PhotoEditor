package v2learning

import (
	"testing"

	"github.com/google/uuid"
)

func TestPreselectTrainingCandidatesPreservesExclusionReason(t *testing.T) {
	approval := uuid.New()
	base := TrainingCandidate{EventID: uuid.New(), ApprovalID: &approval,
		ConsentLive: true, SourceReady: true, BaseReady: true,
		ApprovedReady: true, ChangedPaths: []string{"/ev"}}
	other := base
	other.EventID = uuid.New()
	other.ChangedPaths = []string{"/crop"}
	revoked := base
	revoked.EventID = uuid.New()
	revoked.ConsentLive = false
	selected, excluded := PreselectTrainingCandidates([]TrainingCandidate{
		base, other, revoked})
	if len(selected) != 1 || selected[0].EventID != base.EventID ||
		len(excluded) != 2 || excluded[0].Reason != "NONPARAMETRIC_EDIT" ||
		excluded[1].Reason != "CONSENT_REVOKED" {
		t.Fatalf("selected=%+v excluded=%+v", selected, excluded)
	}
}
