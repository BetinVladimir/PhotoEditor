package review

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestRestorationResultSelectionKeepsRejectedCandidateOutOfApply(t *testing.T) {
	for _, kind := range []string{"portrait.glare", "portrait.open_eyes", "image.reflection"} {
		t.Run(kind, func(t *testing.T) {
			snapshot := CandidateSnapshot{TenantID: uuid.New(), ReviewID: uuid.New(), RunID: uuid.New(), Revision: 1, State: "pending", Kind: kind, Candidate: ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}, Base: ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("b", 64)}}
			actor := uuid.New()
			for _, decision := range []string{"accept", "reject", "skip"} {
				request := Decision{ExpectedRevision: 1, CandidateHash: snapshot.Candidate.SHA256, Decision: decision}
				plan, err := PlanCandidate(snapshot, actor, request)
				if err != nil {
					t.Fatal(err)
				}
				selected, err := CompileRestorationResultSelection(snapshot, actor, request, plan)
				if err != nil {
					t.Fatal(err)
				}
				if selected.DecisionHash != plan.DecisionHash || selected.Route != plan.Route || selected.ReviewID != snapshot.ReviewID {
					t.Fatal("decision identity lost")
				}
				switch decision {
				case "accept":
					if selected.Image == nil || *selected.Image != snapshot.Candidate {
						t.Fatal("candidate not selected")
					}
				case "reject":
					if selected.Image == nil || *selected.Image != snapshot.Base {
						t.Fatal("rejected candidate selected")
					}
				case "skip":
					if selected.Image != nil {
						t.Fatal("skip selected image")
					}
				}
				changed := plan
				changed.Route = "candidate"
				if decision != "accept" {
					if _, err := CompileRestorationResultSelection(snapshot, actor, request, changed); err == nil {
						t.Fatal("substituted route accepted")
					}
				}
				if _, err := CompileRestorationResultSelection(snapshot, uuid.New(), request, plan); err == nil {
					t.Fatal("foreign actor accepted")
				}
			}
			foreign := snapshot
			foreign.Kind = "render.compose"
			request := Decision{ExpectedRevision: 1, CandidateHash: foreign.Candidate.SHA256, Decision: "accept"}
			plan, err := PlanCandidate(foreign, actor, request)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := CompileRestorationResultSelection(foreign, actor, request, plan); err == nil {
				t.Fatal("foreign operation accepted")
			}
		})
	}
}
