package review

import (
	"testing"

	"github.com/google/uuid"
)

func TestPlanCandidateRoutesAndFreezesRefs(t *testing.T) {
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	baseHash := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	s := CandidateSnapshot{TenantID: uuid.New(), ReviewID: uuid.New(), RunID: uuid.New(), Revision: 1, State: "pending", Kind: "inpaint", Candidate: ArtifactRef{uuid.New(), hash}, Base: ArtifactRef{uuid.New(), baseHash}}
	for decision, route := range map[string]string{"accept": "candidate", "reject": "base", "skip": "skip"} {
		p, err := PlanCandidate(s, uuid.New(), Decision{ExpectedRevision: 1, CandidateHash: hash, Decision: decision})
		if err != nil || p.Route != route || p.Revision != 2 || p.Selection.Candidate != s.Candidate || p.Selection.Base != s.Base || p.DecisionHash == "" {
			t.Fatalf("%s: plan=%+v err=%v", decision, p, err)
		}
	}
}

func TestPlanCandidateRejectsDefectFieldsAndStaleInput(t *testing.T) {
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	s := CandidateSnapshot{TenantID: uuid.New(), ReviewID: uuid.New(), RunID: uuid.New(), Revision: 3, State: "pending", Kind: "generation", Candidate: ArtifactRef{uuid.New(), hash}, Base: ArtifactRef{uuid.New(), "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}
	bad := []Decision{
		{ExpectedRevision: 2, CandidateHash: hash, Decision: "accept"},
		{ExpectedRevision: 3, CandidateHash: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", Decision: "accept"},
		{ExpectedRevision: 3, CandidateHash: hash, Decision: "accept", Accepted: []string{hash}},
		{ExpectedRevision: 3, CandidateHash: hash, Decision: "maybe"},
	}
	for _, req := range bad {
		if _, err := PlanCandidate(s, uuid.New(), req); err == nil {
			t.Fatalf("accepted %+v", req)
		}
	}
}
