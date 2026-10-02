package workflow

import "errors"

// AdvanceNode contains trusted persisted state and compiler-resolved bindings.
// BindingsReady means exact input/params artifacts are ready and tenant-owned;
// it must never be inferred merely from prerequisite task success.
type AdvanceNode struct {
	State                TaskState
	SkipReason, Decision string
	BindingsReady        bool
	CancelRequested      bool
}
type AdvanceSnapshot struct {
	Graph                        Graph
	Nodes                        map[string]AdvanceNode
	GraphSealed, CancelRequested bool
	OpenExpansions               int
}
type AdvanceChange struct {
	Key             string
	From, To        TaskState
	Reason          string
	RequestCancel   bool
	PassThroughFrom string
}

// PlanAdvancement computes a deterministic topological batch. Persistence must
// lock the run, revalidate the snapshot and apply all changes transactionally.
// This function neither closes expansions nor fabricates input artifact refs.
func PlanAdvancement(snapshot AdvanceSnapshot, registry Registry) ([]AdvanceChange, error) {
	if snapshot.OpenExpansions < 0 || len(snapshot.Nodes) != len(snapshot.Graph.Nodes) {
		return nil, errors.New("invalid graph snapshot")
	}
	order, err := snapshot.Graph.Order(registry)
	if err != nil {
		return nil, err
	}
	states := make(map[string]AdvanceNode, len(snapshot.Nodes))
	for _, n := range order {
		state, ok := snapshot.Nodes[n.Key]
		if !ok {
			return nil, errors.New("missing node state")
		}
		switch state.State {
		case Blocked, Ready, Queued, Running, Succeeded, RetryWait, Failed, Cancelled, Skipped:
		default:
			return nil, errors.New("invalid task state")
		}
		if state.Decision != "" && state.Decision != "accept" && state.Decision != "reject" && state.Decision != "skip" {
			return nil, errors.New("invalid review decision")
		}
		states[n.Key] = state
	}
	incoming := map[string][]Edge{}
	for _, e := range snapshot.Graph.Edges {
		incoming[e.To] = append(incoming[e.To], e)
	}
	changes := []AdvanceChange{}
	for _, n := range order {
		state := states[n.Key]
		if state.State.Terminal() {
			continue
		}
		if snapshot.CancelRequested {
			if state.State == Running || state.State == Queued {
				if !state.CancelRequested {
					changes = append(changes, AdvanceChange{Key: n.Key, From: state.State, To: state.State, Reason: "cancel_requested", RequestCancel: true})
				}
			} else {
				changes = append(changes, AdvanceChange{Key: n.Key, From: state.State, To: Cancelled, Reason: "cancel_requested"})
			}
			continue
		}
		if !snapshot.GraphSealed || snapshot.OpenExpansions > 0 || state.State != Blocked || state.CancelRequested {
			continue
		}
		waiting, rejected := false, false
		for _, edge := range incoming[n.Key] {
			parent := states[edge.From]
			switch DependencyStatus(Dependency{State: parent.State, SkipReason: parent.SkipReason, Condition: edge.Condition, Decision: parent.Decision}) {
			case "wait":
				waiting = true
			case "reject":
				rejected = true
			}
		}
		if n.Disabled {
			parent := states[n.PassThroughFrom]
			switch DependencyStatus(Dependency{State: parent.State, SkipReason: parent.SkipReason, Condition: "success"}) {
			case "wait":
				waiting = true
			case "reject":
				rejected = true
			}
		}
		// A failed required predecessor is definitive even while another is active.
		if rejected {
			changes = append(changes, AdvanceChange{Key: n.Key, From: Blocked, To: Skipped, Reason: "upstream_failed"})
			state.State = Skipped
			state.SkipReason = "upstream_failed"
			states[n.Key] = state
			continue
		}
		if waiting || !state.BindingsReady {
			continue
		}
		if n.Disabled {
			changes = append(changes, AdvanceChange{Key: n.Key, From: Blocked, To: Skipped, Reason: "disabled", PassThroughFrom: n.PassThroughFrom})
			state.State = Skipped
			state.SkipReason = "disabled"
			states[n.Key] = state
		} else {
			changes = append(changes, AdvanceChange{Key: n.Key, From: Blocked, To: Ready})
		}
	}
	return changes, nil
}
