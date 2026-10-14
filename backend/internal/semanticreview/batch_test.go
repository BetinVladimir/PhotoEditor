package semanticreview

import (
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestBatchPreservesOmittedTargetsAndParentUntilExplicitClose(t *testing.T) {
	approve, reject, omitted, done := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	sha := strings.Repeat("a", 64)
	child := uuid.New()
	batch := uuid.New()
	decision := ApplyDecision{sha, []TargetDecision{{approve, "approve"}, {reject, "reject"}}, "4"}
	states := map[uuid.UUID]string{approve: "proposed", reject: "proposed", omitted: "proposed", done: "applied"}
	result, err := PlanBatch(decision, sha, "4", "waiting_review", states, batch, &child)
	if err != nil || result.Remaining != 1 || result.NextRevision != "5" || result.ParentState != "waiting_review" || len(result.Targets) != 2 {
		t.Fatal(result, err)
	}
	for _, patch := range result.Targets {
		if patch.TargetID == approve {
			if patch.State != "queued" || patch.ChildRunID == nil || *patch.ChildRunID != child {
				t.Fatal(patch)
			}
		} else if patch.State != "rejected" || patch.ChildRunID != nil {
			t.Fatal(patch)
		}
	}
	if states[approve] != "proposed" || states[omitted] != "proposed" {
		t.Fatal("input mutation")
	}
	decision.TargetDecisions = []TargetDecision{{omitted, "reject"}}
	result, err = PlanBatch(decision, sha, "4", "waiting_review", states, batch, nil)
	if err != nil || result.Targets[0].ChildRunID != nil {
		t.Fatal(result, err)
	}
	for _, state := range []string{"queued", "applied", "failed", "cancelled", "rejected", "unsupported"} {
		states[omitted] = state
		if _, err := PlanBatch(decision, sha, "4", "waiting_review", states, batch, nil); err == nil {
			t.Fatal("repeated decision", state)
		}
	}
}
func TestBatchRequiresChildExactlyForApprovals(t *testing.T) {
	id := uuid.New()
	sha := strings.Repeat("a", 64)
	states := map[uuid.UUID]string{id: "proposed"}
	batch := uuid.New()
	child := uuid.New()
	decision := ApplyDecision{sha, []TargetDecision{{id, "approve"}}, "1"}
	if _, err := PlanBatch(decision, sha, "1", "waiting_review", states, batch, nil); err == nil {
		t.Fatal("missing child")
	}
	decision.TargetDecisions[0].Decision = "reject"
	if _, err := PlanBatch(decision, sha, "1", "waiting_review", states, batch, &child); err == nil {
		t.Fatal("unexpected child")
	}
	result, err := PlanBatch(decision, sha, "1", "waiting_review", states, batch, nil)
	if err != nil || result.Remaining != 0 || result.ParentState != "waiting_review" {
		t.Fatal(result, err)
	}
}
