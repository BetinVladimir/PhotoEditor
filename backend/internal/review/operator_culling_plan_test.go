package review

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
)

func operatorCullControl(t *testing.T) (OperatorCullingScope, CullingDecision) {
	t.Helper()
	item := uuid.New()
	proposal := ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	raw, e := canonical.Marshal(cullingProjection{Groups: ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("b", 64)}, Proposal: proposal, NeverDelete: true, Items: []CullingItem{{ItemID: item, AssetID: uuid.New(), VersionID: uuid.New(), SceneID: "scene", ExactGroupID: "exact", BurstGroupID: "burst", Rank: 1, Score: .7, Recommended: true, EyeReview: true, EyeState: "unknown"}}})
	if e != nil {
		t.Fatal(e)
	}
	return OperatorCullingScope{Tenant: uuid.New(), Actor: uuid.New(), Run: uuid.New(), Review: uuid.New(), Snapshot: uuid.New(), SnapshotDigest: strings.Repeat("c", 64), CandidateHash: proposal.SHA256, Projection: raw}, CullingDecision{ExpectedRevision: 1, CandidateHash: proposal.SHA256, Selected: []uuid.UUID{item}, Confirmed: []uuid.UUID{item}, NeverDelete: true}
}
func TestOperatorCullingUsesExplicitEyesAndExactPlanner(t *testing.T) {
	s, r := operatorCullControl(t)
	plan, e := PlanOperatorCulling(s, r)
	if e != nil || len(plan.Selected) != 1 || !plan.NeverDelete || len(plan.DecisionHash) != 64 {
		t.Fatalf("control %v %+v", e, plan)
	}
	r.Confirmed = []uuid.UUID{}
	if _, e = PlanOperatorCulling(s, r); e == nil {
		t.Fatal("unconfirmed eyes accepted")
	}
}
func TestOperatorCullingScopeAndRevisionNegatives(t *testing.T) {
	for _, name := range []string{"actor", "digest", "revision", "foreign", "delete", "hash"} {
		t.Run(name, func(t *testing.T) {
			s, r := operatorCullControl(t)
			switch name {
			case "actor":
				s.Actor = uuid.Nil
			case "digest":
				s.SnapshotDigest = "bad"
			case "revision":
				r.ExpectedRevision = 2
			case "foreign":
				r.Selected = []uuid.UUID{uuid.New()}
			case "delete":
				r.NeverDelete = false
			case "hash":
				s.CandidateHash = strings.Repeat("d", 64)
			}
			if _, e := PlanOperatorCulling(s, r); e == nil {
				t.Fatal("invalid decision accepted")
			}
		})
	}
}
