package workflow

import (
	"reflect"
	"testing"
)

func advancementFixture(t *testing.T) (AdvanceSnapshot, Registry) {
	t.Helper()
	r, e := LoadRegistry()
	if e != nil {
		t.Fatal(e)
	}
	return AdvanceSnapshot{Graph: Graph{Nodes: []Node{{Key: "a", Operation: "asset.decode"}, {Key: "b", Operation: "image.color", Disabled: true, PassThroughFrom: "a"}, {Key: "c", Operation: "image.local"}}, Edges: []Edge{{"a", "b", "success"}, {"b", "c", "success"}}}, Nodes: map[string]AdvanceNode{"a": {State: Succeeded}, "b": {State: Blocked, BindingsReady: true}, "c": {State: Blocked, BindingsReady: true}}, GraphSealed: true}, r
}
func TestAdvanceDisabledChainAndFailure(t *testing.T) {
	s, r := advancementFixture(t)
	changes, e := PlanAdvancement(s, r)
	if e != nil || len(changes) != 2 || changes[0].Reason != "disabled" || changes[0].PassThroughFrom != "a" || changes[1].To != Ready {
		t.Fatal(changes, e)
	}
	if s.Nodes["b"].State != Blocked {
		t.Fatal("snapshot mutated")
	}
	s.Nodes["a"] = AdvanceNode{State: Failed}
	changes, e = PlanAdvancement(s, r)
	if e != nil || len(changes) != 2 || changes[0].Reason != "upstream_failed" || changes[1].Reason != "upstream_failed" {
		t.Fatal(changes, e)
	}
}
func TestAdvanceWaitsForBindingsReviewAndSeal(t *testing.T) {
	for _, scenario := range []string{"bindings", "review", "unsealed", "expansion"} {
		t.Run(scenario, func(t *testing.T) {
			s, r := advancementFixture(t)
			switch scenario {
			case "bindings":
				s.Nodes["b"] = AdvanceNode{State: Blocked}
			case "review":
				s.Graph.Edges[0].Condition = "accepted"
			case "unsealed":
				s.GraphSealed = false
			case "expansion":
				s.OpenExpansions = 1
			}
			got, e := PlanAdvancement(s, r)
			if e != nil || len(got) != 0 {
				t.Fatal(got, e)
			}
		})
	}
}
func TestAdvanceCancelAndDeterministicOrder(t *testing.T) {
	s, r := advancementFixture(t)
	s.CancelRequested = true
	s.GraphSealed = false
	s.Nodes["b"] = AdvanceNode{State: Running}
	got, e := PlanAdvancement(s, r)
	if e != nil || len(got) != 2 || !got[0].RequestCancel || got[1].To != Cancelled {
		t.Fatal(got, e)
	}
	s.Graph.Nodes[0], s.Graph.Nodes[2] = s.Graph.Nodes[2], s.Graph.Nodes[0]
	again, e := PlanAdvancement(s, r)
	if e != nil || !reflect.DeepEqual(got, again) {
		t.Fatal("unstable order")
	}
}
func TestUnsealedPendingReviewRemainsVisible(t *testing.T) {
	if got := AggregateRun(RunSummary{PendingReviews: 1, OpenExpansions: 1}); got != "waiting_review" {
		t.Fatal(got)
	}
}

func TestDisabledPassThroughRequiresSuccessfulSourceEvenOnTerminalEdge(t *testing.T) {
	s, r := advancementFixture(t)
	s.Graph.Edges[0].Condition = "terminal"
	s.Nodes["a"] = AdvanceNode{State: Failed}
	changes, err := PlanAdvancement(s, r)
	if err != nil || len(changes) != 2 || changes[0].Reason != "upstream_failed" || changes[0].PassThroughFrom != "" {
		t.Fatal("failed input became identity", changes, err)
	}
}
