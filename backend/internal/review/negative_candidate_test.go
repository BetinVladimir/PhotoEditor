package review

import (
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestNegativeCandidateDecisionPreservesExactBeforeAfterAndActor(t *testing.T) {
	sha := strings.Repeat("a", 64)
	snapshot := CandidateSnapshot{TenantID: uuid.New(), ReviewID: uuid.New(), RunID: uuid.New(), Revision: 1, State: "pending", Kind: "optical.negative", Candidate: ArtifactRef{ID: uuid.New(), SHA256: sha}, Base: ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("b", 64)}}
	actor := uuid.New()
	for _, decision := range []string{"accept", "reject", "skip"} {
		t.Run(decision, func(t *testing.T) {
			request := Decision{ExpectedRevision: 1, CandidateHash: sha, Decision: decision}
			plan, err := PlanCandidate(snapshot, actor, request)
			if err != nil || plan.Selection.Candidate != snapshot.Candidate || plan.Selection.Base != snapshot.Base || plan.Revision != 2 {
				t.Fatal("negative candidate binding lost", err)
			}
			expected := map[string]string{"accept": "candidate", "reject": "base", "skip": "skip"}[decision]
			if plan.Route != expected {
				t.Fatal("decision routed incorrectly")
			}
			other, err := PlanCandidate(snapshot, uuid.New(), request)
			if err != nil || other.DecisionHash == plan.DecisionHash {
				t.Fatal("actor not bound to decision hash", err)
			}
			request.CandidateHash = strings.Repeat("c", 64)
			if _, err := PlanCandidate(snapshot, actor, request); err == nil {
				t.Fatal("stale candidate accepted")
			}
		})
	}
}
