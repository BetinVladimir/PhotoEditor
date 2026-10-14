package workflow

import (
	"context"
	"errors"
	"photoeditor/backend/internal/v2input"
	"reflect"
	"testing"
)

type continuationCoordinatorStore struct {
	calls                 *[]string
	activated, registered bool
	fail                  string
}

func (s continuationCoordinatorStore) step(name string) error {
	*s.calls = append(*s.calls, name)
	if s.fail == name {
		return errors.New(name)
	}
	return nil
}
func (s continuationCoordinatorStore) Activated(context.Context) (bool, error) {
	return s.activated, s.step("activated")
}
func (s continuationCoordinatorStore) Registered(context.Context) (bool, error) {
	return s.registered, s.step("registered")
}
func (s continuationCoordinatorStore) Create(context.Context) error { return s.step("create") }
func (s continuationCoordinatorStore) Commit(_ context.Context, graph *v2input.GraphTask) error {
	if s.registered != (graph == nil) {
		panic("wrong recovery graph")
	}
	return s.step("commit")
}
func exerciseContinuationCoordinator(t *testing.T, frozen FrozenDeliveryContinuationTask) {
	t.Helper()
	for _, scenario := range []string{"fresh", "activated", "registered", "create", "publish", "commit"} {
		calls := []string{}
		store := continuationCoordinatorStore{calls: &calls, activated: scenario == "activated", registered: scenario == "registered", fail: scenario}
		if scenario != "create" && scenario != "commit" {
			store.fail = ""
		}
		publisher := deliveryCoordinatorPublisher{calls: &calls, fail: scenario == "publish"}
		recovered, err := frozen.admitContinuation(context.Background(), store, publisher)
		switch scenario {
		case "activated":
			if err != nil || !recovered || !reflect.DeepEqual(calls, []string{"activated"}) {
				t.Fatal(scenario, calls, err)
			}
		case "registered":
			if err != nil || recovered || !reflect.DeepEqual(calls, []string{"activated", "registered", "commit"}) {
				t.Fatal(scenario, calls, err)
			}
		case "fresh":
			if err != nil || recovered || len(calls) < 5 || !reflect.DeepEqual(calls[:3], []string{"activated", "registered", "create"}) || calls[len(calls)-1] != "commit" {
				t.Fatal(scenario, calls, err)
			}
			for _, call := range calls[3 : len(calls)-1] {
				if call != "publish" {
					t.Fatal("IO outside phase", calls)
				}
			}
		default:
			if err == nil || recovered {
				t.Fatal("failure accepted", scenario)
			}
			if scenario == "create" && !reflect.DeepEqual(calls, []string{"activated", "registered", "create"}) {
				t.Fatal(calls)
			}
			if scenario == "publish" && calls[len(calls)-1] != "publish" {
				t.Fatal(calls)
			}
			if scenario == "commit" && calls[len(calls)-1] != "commit" {
				t.Fatal(calls)
			}
		}
	}
}
