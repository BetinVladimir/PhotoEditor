package workflow

import (
	"context"
	"errors"
	"testing"
)

func TestObserveScaleFromLedgerRejectsClosedAndInvalidPool(t *testing.T) {
	if _, _, err := (&CapacityRuntime{}).ObserveScaleFromLedger(context.Background(), "gpu"); !errors.Is(err, ErrCapacityLeaderUnavailable) {
		t.Fatal(err)
	}
	runtime := &CapacityRuntime{leader: &CapacityLeader{},
		dispatchers: []*CapacityDispatcher{{}, {}, {}}}
	if _, _, err := runtime.ObserveScaleFromLedger(context.Background(), "invalid"); err == nil {
		t.Fatal("invalid pool reached leader")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := runtime.ObserveScaleFromLedger(ctx, "gpu"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
