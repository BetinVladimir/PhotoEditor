package workflow

import (
	"context"
	"errors"
	"testing"
	"time"
)

type runtimePass func(context.Context) (bool, error)

func (f runtimePass) ProcessOne(ctx context.Context) (bool, error) { return f(ctx) }

func TestRuntimeFailureWithdrawsBeforeLoopBackoff(t *testing.T) {
	failure := errors.New("export adoption failed")
	state := new(ExpansionLoopState)
	state.Ready.Store(true)
	called := 0
	job := runtimeFailureWithdrawal{
		Job:   runtimePass(func(context.Context) (bool, error) { return false, failure }),
		State: state,
		Withdraw: func(ctx context.Context) error {
			called++
			if state.Ready.Load() {
				t.Fatal("runtime still ready during withdrawal")
			}
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > 3*time.Second || time.Until(deadline) <= 0 {
				t.Fatal("withdrawal has no bounded context")
			}
			return errors.New("database temporarily unavailable")
		},
	}
	done, err := job.ProcessOne(context.Background())
	if done || !errors.Is(err, failure) || called != 1 {
		t.Fatal("worker failure or withdrawal lost", done, err, called)
	}
}

func TestRuntimeFailureWithdrawalSkipsSuccessAndCancellation(t *testing.T) {
	state := new(ExpansionLoopState)
	state.Ready.Store(true)
	called := 0
	withdraw := func(context.Context) error { called++; return nil }
	success := runtimeFailureWithdrawal{Job: runtimePass(func(context.Context) (bool, error) {
		return true, nil
	}), State: state, Withdraw: withdraw}
	if done, err := success.ProcessOne(context.Background()); !done || err != nil || called != 0 || !state.Ready.Load() {
		t.Fatal("healthy pass withdrew runtime", done, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stopped := runtimeFailureWithdrawal{Job: runtimePass(func(context.Context) (bool, error) {
		return false, context.Canceled
	}), State: state, Withdraw: withdraw}
	if _, err := stopped.ProcessOne(ctx); !errors.Is(err, context.Canceled) || called != 0 {
		t.Fatal("shutdown performed redundant failure withdrawal", err, called)
	}
}
