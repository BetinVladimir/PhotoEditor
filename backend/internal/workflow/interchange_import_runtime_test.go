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

func TestInterchangeImportRuntimeRequiresCompletedHealthyPass(t *testing.T) {
	if interchangeImportLoopEligible(nil) {
		t.Fatal("nil loop advertised import runtime")
	}
	state := new(ExpansionLoopState)
	state.Started.Store(true)
	if interchangeImportLoopEligible(state) {
		t.Fatal("first import pass still running advertised runtime")
	}
	state.Ready.Store(true)
	if !interchangeImportLoopEligible(state) {
		t.Fatal("completed healthy pass did not advertise runtime")
	}
	state.Ready.Store(false)
	state.Failures.Add(1)
	if interchangeImportLoopEligible(state) {
		t.Fatal("failed pass kept import runtime available")
	}
	state.Ready.Store(true)
	if !interchangeImportLoopEligible(state) {
		t.Fatal("recovered healthy import loop unavailable")
	}
	state.Started.Store(false)
	if interchangeImportLoopEligible(state) {
		t.Fatal("stopped loop kept import runtime available")
	}
}
