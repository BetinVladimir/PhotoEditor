package workflow

import (
	"github.com/google/uuid"
	"reflect"
	"testing"
	"time"
)

func TestRetryBudgets(t *testing.T) {
	now := time.Unix(1000, 0)
	c := RetryContext{TaskID: uuid.New(), Attempt: 1, Now: now}
	for i := 0; i < 3; i++ {
		d := DecideRetry(c, Failure{"DEPENDENCY_UNAVAILABLE", true})
		if d.State != RetryWait || d.RetryCount != i+1 {
			t.Fatal(d)
		}
		base := []time.Duration{5 * time.Second, 30 * time.Second, 120 * time.Second}[i]
		delay := d.NextAttemptAt.Sub(now)
		if delay < base*8/10 || delay > base*12/10 {
			t.Fatal(delay)
		}
		repeat := DecideRetry(c, Failure{"DEPENDENCY_UNAVAILABLE", true})
		if !reflect.DeepEqual(d, repeat) {
			t.Fatal("nondeterministic")
		}
		c.Attempt++
		c.RetryCount = d.RetryCount
	}
	if d := DecideRetry(c, Failure{"DEPENDENCY_UNAVAILABLE", true}); d.State != Failed {
		t.Fatal(d)
	}
}
func TestOOMFallbackAndAttemptCap(t *testing.T) {
	c := RetryContext{TaskID: uuid.New(), Attempt: 1, CanReduce: true, CanFallbackCPU: true, Now: time.Now()}
	for i := 0; i < 2; i++ {
		d := DecideRetry(c, Failure{"RESOURCE_OOM", true})
		if !d.ReduceResources || d.OOMCount != i+1 || d.FallbackCPU {
			t.Fatal(d)
		}
		c.Attempt++
		c.OOMCount = d.OOMCount
	}
	d := DecideRetry(c, Failure{"RESOURCE_OOM", true})
	if !d.FallbackCPU || d.RetryCount != 1 || d.ReduceResources {
		t.Fatal(d)
	}
	c.Attempt = 6
	if d = DecideRetry(c, Failure{"RESOURCE_OOM", true}); d.State != Failed {
		t.Fatal(d)
	}
	c.CancelRequested = true
	if d = DecideRetry(c, Failure{"RESOURCE_OOM", true}); d.State != Cancelled {
		t.Fatal(d)
	}
}
func TestTerminalCannotAutomaticallyResume(t *testing.T) {
	for _, from := range []TaskState{Succeeded, Failed, Skipped, Cancelled} {
		for _, to := range []TaskState{Blocked, Ready, Queued, Running, RetryWait} {
			if CanTransition(from, to) {
				t.Fatal(from, to)
			}
		}
	}
	if !CanTransition(Running, Succeeded) || CanTransition(Blocked, Succeeded) {
		t.Fatal("invalid transitions")
	}
}
func TestDependenciesAndAggregate(t *testing.T) {
	for _, c := range []struct {
		d    Dependency
		want string
	}{{Dependency{Skipped, "disabled", "success", ""}, "allow"}, {Dependency{Skipped, "upstream_failed", "success", ""}, "reject"}, {Dependency{Succeeded, "", "accepted", ""}, "wait"}, {Dependency{Succeeded, "", "accepted", "accept"}, "allow"}, {Dependency{Succeeded, "", "rejected", "reject"}, "allow"}, {Dependency{Failed, "", "terminal", ""}, "allow"}} {
		if got := DependencyStatus(c.d); got != c.want {
			t.Fatal(c, got)
		}
	}
	if got := AggregateRun(RunSummary{GraphSealed: true, AcceptedOutputs: 2, FailedItems: 1}); got != "completed_with_errors" {
		t.Fatal(got)
	}
	if got := AggregateRun(RunSummary{GraphSealed: true, PendingReviews: 1}); got != "waiting_review" {
		t.Fatal(got)
	}
	if got := AggregateRun(RunSummary{GraphSealed: true, OpenExpansions: 1}); got != "running" {
		t.Fatal(got)
	}
}
func TestDAGValidation(t *testing.T) {
	r, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	g := Graph{Nodes: []Node{{Key: "b", Operation: "image.color"}, {Key: "a", Operation: "asset.decode"}}, Edges: []Edge{{"a", "b", "success"}}}
	order, err := g.Order(r)
	if err != nil || order[0].Key != "a" {
		t.Fatal(order, err)
	}
	g.Edges = append(g.Edges, Edge{"b", "a", "success"})
	if _, err = g.Order(r); err == nil {
		t.Fatal("cycle accepted")
	}
	g.Edges = []Edge{{"a", "b", "success"}, {"a", "b", "accepted"}}
	if _, err = g.Order(r); err == nil {
		t.Fatal("duplicate accepted")
	}
}
