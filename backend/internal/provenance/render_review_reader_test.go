package provenance

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"strings"
	"testing"
)

func TestSigningCandidateDecisionRecomputesImmutableAcceptance(t *testing.T) {
	for _, mode := range []string{"valid", "hash", "actor", "candidate", "base", "selection", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			sha := strings.Repeat("a", 64)
			actor := uuid.New()
			snapshot := review.CandidateSnapshot{TenantID: uuid.New(), ReviewID: uuid.New(), RunID: uuid.New(), Revision: 1, State: "pending", Kind: "render", Candidate: review.ArtifactRef{ID: uuid.New(), SHA256: sha}, Base: review.ArtifactRef{ID: uuid.New(), SHA256: sha}}
			plan, err := review.PlanCandidate(snapshot, actor, review.Decision{ExpectedRevision: 1, CandidateHash: sha, Decision: "accept"})
			if err != nil {
				t.Fatal(err)
			}
			raw, err := canonical.Marshal(plan.Selection)
			if err != nil {
				t.Fatal(err)
			}
			hash := plan.DecisionHash
			switch mode {
			case "hash":
				hash = strings.Repeat("b", 64)
			case "actor":
				actor = uuid.New()
			case "candidate":
				snapshot.Candidate.ID = uuid.New()
			case "base":
				snapshot.Base.ID = uuid.New()
			case "selection":
				raw = []byte(`{}`)
			case "duplicate":
				raw = append(raw[:len(raw)-1], []byte(`,"revision":2}`)...)
			}
			_, err = verifySigningCandidateDecision(snapshot, actor, raw, hash)
			if mode == "valid" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil {
				t.Fatal("substituted candidate decision accepted")
			}
		})
	}
}
