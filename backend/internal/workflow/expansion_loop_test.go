package workflow

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type loopJob func(context.Context) (bool, error)

func (f loopJob) ProcessOne(c context.Context) (bool, error) { return f(c) }
func TestExpansionLoopCancelsInflightAndClearsReady(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	state := new(ExpansionLoopState)
	state.Ready.Store(true)
	done := make(chan error, 1)
	go func() {
		done <- RunExpansionLoop(ctx, loopJob(func(c context.Context) (bool, error) { close(entered); <-c.Done(); return false, c.Err() }), state, time.Millisecond)
	}()
	<-entered
	cancel()
	select {
	case err := <-done:
		if err != nil || state.Ready.Load() || state.Failures.Load() != 0 {
			t.Fatal("shutdown counted as failure", err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown hung")
	}
}
func TestExpansionLoopFailureThenRecovery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := new(ExpansionLoopState)
	var calls atomic.Int32
	job := loopJob(func(context.Context) (bool, error) {
		switch calls.Add(1) {
		case 1:
			return false, errors.New("transient")
		case 2:
			if state.Failures.Load() != 1 || state.Ready.Load() {
				t.Error("failure was hidden")
			}
			return true, nil
		default:
			if !state.Ready.Load() || state.Materialized.Load() != 1 {
				t.Error("recovery not recorded")
			}
			cancel()
			return false, nil
		}
	})
	if err := RunExpansionLoop(ctx, job, state, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if state.Ready.Load() || state.Failures.Load() != 1 || state.Materialized.Load() != 1 {
		t.Fatal("wrong counters")
	}
}
