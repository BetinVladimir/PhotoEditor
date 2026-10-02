package review

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/domain"
)

func fixture() (DefectSnapshot, uuid.UUID, Decision) {
	ref := func(c string) ArtifactRef { return ArtifactRef{uuid.New(), strings.Repeat(c, 64)} }
	s := DefectSnapshot{TenantID: uuid.New(), ReviewID: uuid.New(), RunID: uuid.New(), Revision: 7, State: "pending",
		Proposal: ref("a"), Image: ref("b"), LabelMap: ref("c"), CandidateIDs: []string{strings.Repeat("1", 64), strings.Repeat("2", 64), strings.Repeat("3", 64)}}
	return s, uuid.New(), Decision{ExpectedRevision: 7, CandidateHash: s.Proposal.SHA256, Decision: "accept", Accepted: []string{s.CandidateIDs[1], s.CandidateIDs[0]}}
}

func TestSelectionFreezesOnlyExplicitCandidates(t *testing.T) {
	s, actor, req := fixture()
	plan, err := PlanDefects(s, actor, req)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Route != "repair" || plan.Revision != 8 || len(plan.Selection.Accepted) != 2 || len(plan.Selection.Rejected) != 0 {
		t.Fatalf("invalid plan: %+v", plan)
	}
	if req.Accepted[0] != s.CandidateIDs[1] {
		t.Fatal("mutated request")
	}
	req.Accepted[0] = s.CandidateIDs[2]
	if plan.Selection.Accepted[1] != s.CandidateIDs[1] {
		t.Fatal("aliased request")
	}
	b, err := json.Marshal(plan.Selection)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err = json.Unmarshal(b, &wire); err != nil {
		t.Fatal(err)
	}
	if len(wire) != 7 || string(wire["version"]) != `"defect_review_v1"` || string(wire["rejected_candidate_ids"]) != "[]" {
		t.Fatalf("incompatible worker selection: %s", b)
	}
	if s.Revision != 7 || s.State != "pending" {
		t.Fatal("mutated snapshot")
	}
}

func TestDecisionDigestBindsContextAndIgnoresSetOrder(t *testing.T) {
	s, actor, req := fixture()
	a, err := PlanDefects(s, actor, req)
	if err != nil {
		t.Fatal(err)
	}
	req.Accepted[0], req.Accepted[1] = req.Accepted[1], req.Accepted[0]
	b, err := PlanDefects(s, actor, req)
	if err != nil || a.DecisionHash != b.DecisionHash {
		t.Fatal("order changed hash", err)
	}
	for _, change := range []func(*DefectSnapshot){
		func(s *DefectSnapshot) { s.ReviewID = uuid.New() }, func(s *DefectSnapshot) { s.RunID = uuid.New() },
		func(s *DefectSnapshot) { s.TenantID = uuid.New() }, func(s *DefectSnapshot) { s.LabelMap.ID = uuid.New() },
		func(s *DefectSnapshot) { s.Image.SHA256 = strings.Repeat("d", 64) },
	} {
		changed := s
		change(&changed)
		p, e := PlanDefects(changed, actor, req)
		if e != nil || p.DecisionHash == a.DecisionHash {
			t.Fatal("context not bound", e)
		}
	}
	b, err = PlanDefects(s, uuid.New(), req)
	if err != nil || a.DecisionHash == b.DecisionHash {
		t.Fatal("actor not bound", err)
	}
}

func TestInvalidReviewDecisions(t *testing.T) {
	tests := []struct {
		name, code string
		mutate     func(*DefectSnapshot, *Decision)
	}{
		{"stale revision", "REVISION_CONFLICT", func(s *DefectSnapshot, r *Decision) { r.ExpectedRevision-- }},
		{"decided", "REVISION_CONFLICT", func(s *DefectSnapshot, r *Decision) { s.State = "decided" }},
		{"stale proposal", "STALE_DEFECT_REVIEW", func(s *DefectSnapshot, r *Decision) { r.CandidateHash = strings.Repeat("f", 64) }},
		{"unknown candidate", "UNKNOWN_DEFECT_CANDIDATE", func(s *DefectSnapshot, r *Decision) { r.Accepted = []string{strings.Repeat("f", 64)} }},
		{"duplicate", "VALIDATION_FAILED", func(s *DefectSnapshot, r *Decision) { r.Accepted = append(r.Accepted, r.Accepted[0]) }},
		{"conflict", "VALIDATION_FAILED", func(s *DefectSnapshot, r *Decision) { r.Rejected = []string{r.Accepted[0]} }},
		{"reject with selection", "VALIDATION_FAILED", func(s *DefectSnapshot, r *Decision) { r.Decision = "reject" }},
		{"invalid verb", "VALIDATION_FAILED", func(s *DefectSnapshot, r *Decision) { r.Decision = "approve" }},
		{"invalid digest", "VALIDATION_FAILED", func(s *DefectSnapshot, r *Decision) { r.CandidateHash = "wrong" }},
		{"overflow", "INTERNAL_ERROR", func(s *DefectSnapshot, r *Decision) { s.Revision = maxRevision; r.ExpectedRevision = maxRevision }},
		{"bad catalog", "INTERNAL_ERROR", func(s *DefectSnapshot, r *Decision) { s.CandidateIDs = append(s.CandidateIDs, s.CandidateIDs[0]) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, actor, req := fixture()
			tt.mutate(&s, &req)
			p, err := PlanDefects(s, actor, req)
			if err == nil || domain.AsError(err).Code != tt.code || p.DecisionHash != "" {
				t.Fatalf("unexpected result %+v %v", p, err)
			}
		})
	}
}

func TestNoImplicitAcceptance(t *testing.T) {
	for _, verb := range []string{"accept", "reject", "skip"} {
		s, actor, req := fixture()
		req.Decision = verb
		req.Accepted = nil
		p, err := PlanDefects(s, actor, req)
		if err != nil {
			t.Fatal(err)
		}
		want := "base"
		if verb == "skip" {
			want = "skip"
		}
		if p.Route != want || len(p.Selection.Accepted) != 0 {
			t.Fatalf("implicit repair: %+v", p)
		}
	}
}
