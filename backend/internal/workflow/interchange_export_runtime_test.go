package workflow

import (
	"context"
	"errors"
	"testing"
)

func TestInterchangeExportRuntimeSQLRequiresSchedulerPool(t *testing.T) {
	if err := validateInterchangeExportRuntimeSQL(context.Background(), nil); err == nil {
		t.Fatal("missing export runtime database accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := validateInterchangeExportRuntimeSQL(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled export runtime startup continued", err)
	}
}

func TestInterchangeExportRuntimeRequiresCompletedHealthyPass(t *testing.T) {
	if interchangeExportLoopEligible(nil) {
		t.Fatal("nil loop advertised export runtime")
	}
	state := new(ExpansionLoopState)
	state.Started.Store(true)
	if interchangeExportLoopEligible(state) {
		t.Fatal("first export pass still running advertised runtime")
	}
	state.Ready.Store(true)
	if !interchangeExportLoopEligible(state) {
		t.Fatal("healthy export loop unavailable")
	}
	state.Ready.Store(false)
	state.Failures.Add(1)
	if interchangeExportLoopEligible(state) {
		t.Fatal("failed export pass kept runtime available")
	}
	state.Started.Store(false)
	if interchangeExportLoopEligible(state) {
		t.Fatal("stopped export loop kept runtime available")
	}
}
