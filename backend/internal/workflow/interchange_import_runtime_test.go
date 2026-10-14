package workflow

import (
	"context"
	"errors"
	"testing"
)

func TestInterchangeImportRuntimeSQLRequiresSchedulerPool(t *testing.T) {
	if err := validateInterchangeImportRuntimeSQL(context.Background(), nil); err == nil {
		t.Fatal("missing import runtime database accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := validateInterchangeImportRuntimeSQL(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled import runtime startup continued", err)
	}
}

func TestInterchangeImportLoopEligibilityDuringFirstLongJob(t *testing.T) {
	state := &ExpansionLoopState{}
	if !interchangeImportLoopEligible(state) {
		t.Fatal("installed loop unavailable while first job is active")
	}
	state.Failures.Add(1)
	if interchangeImportLoopEligible(state) {
		t.Fatal("failed import loop advertised")
	}
	state.Ready.Store(true)
	if !interchangeImportLoopEligible(state) {
		t.Fatal("recovered import loop unavailable")
	}
}
