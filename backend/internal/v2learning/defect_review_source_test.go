package v2learning

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func TestReviewedDefectSourceRequiresExactDecidedProducer(t *testing.T) {
	ref := func(char string) review.ArtifactRef {
		return review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat(char, 64)}
	}
	snapshot := review.DefectSnapshot{TenantID: uuid.New(), ReviewID: uuid.New(), RunID: uuid.New(),
		Revision: 1, State: "pending", Proposal: ref("a"), Image: ref("b"),
		LabelMap: ref("c"), CandidateIDs: []string{strings.Repeat("d", 64)}}
	actor := uuid.New()
	plan, err := review.PlanDefects(snapshot, actor, review.Decision{
		ExpectedRevision: 1, CandidateHash: snapshot.Proposal.SHA256,
		Decision: "accept", Accepted: []string{snapshot.CandidateIDs[0]},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := canonical.Marshal(plan.Selection)
	if err != nil {
		t.Fatal(err)
	}
	selectionDigest := sha256.Sum256(raw)
	source := ReviewedDefectSource{
		Decision: DefectHoldoutDecision{TenantID: snapshot.TenantID, ReviewID: snapshot.ReviewID,
			RunID: snapshot.RunID, ActorID: actor, Decision: "accept", Route: plan.Route,
			DecisionHash: plan.DecisionHash, Revision: plan.Revision},
		Image:          v2input.Ref{ArtifactID: snapshot.Image.ID, SHA256: snapshot.Image.SHA256},
		Proposal:       v2input.Ref{ArtifactID: snapshot.Proposal.ID, SHA256: snapshot.Proposal.SHA256},
		LabelMap:       v2input.Ref{ArtifactID: snapshot.LabelMap.ID, SHA256: snapshot.LabelMap.SHA256},
		Selection:      v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(selectionDigest[:])},
		Resolved:       v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("f", 64)},
		Params:         v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("e", 64)},
		ProposalTaskID: uuid.New(), RepairTaskID: uuid.New(),
	}
	if err != nil || validateReviewedDefectSource(&source, raw, snapshot.ReviewID) != nil {
		t.Fatal("current review decision rejected", err)
	}
	for _, change := range []func(*ReviewedDefectSource){
		func(s *ReviewedDefectSource) { s.Decision.Revision++ },
		func(s *ReviewedDefectSource) { s.Decision.DecisionHash = strings.Repeat("0", 64) },
		func(s *ReviewedDefectSource) { s.Proposal.ArtifactID = uuid.New() },
		func(s *ReviewedDefectSource) { s.Params.ArtifactID = s.Image.ArtifactID },
		func(s *ReviewedDefectSource) { s.RepairTaskID = s.ProposalTaskID },
	} {
		candidate := source
		change(&candidate)
		if validateReviewedDefectSource(&candidate, raw, snapshot.ReviewID) == nil {
			t.Fatal("substituted review decision accepted")
		}
	}
	if validateReviewedDefectSource(&source, append(raw, ' '), uuid.New()) == nil ||
		validateReviewedDefectSource(&source, []byte(`{"version":"defect_review_v1","extra":1}`), snapshot.ReviewID) == nil {
		t.Fatal("unscoped or unknown review fields accepted")
	}
}
