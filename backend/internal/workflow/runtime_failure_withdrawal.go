package workflow

import (
	"context"
	"errors"
	"time"
)

// runtimeFailureWithdrawal closes admission as soon as an installed V30 loop
// fails. The periodic maintainer and SQL lease expiry remain independent
// fallback fences if this best-effort withdrawal cannot reach the database.
type runtimeFailureWithdrawal struct {
	Job      ExpansionJob
	State    *ExpansionLoopState
	Withdraw func(context.Context) error
}

func (w runtimeFailureWithdrawal) ProcessOne(ctx context.Context) (bool, error) {
	if w.Job == nil || w.State == nil || w.Withdraw == nil {
		return false, errors.New("invalid runtime failure withdrawal")
	}
	done, err := w.Job.ProcessOne(ctx)
	if err != nil && ctx.Err() == nil {
		w.State.Ready.Store(false)
		bounded, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		_ = w.Withdraw(bounded)
	}
	return done, err
}

var _ ExpansionJob = runtimeFailureWithdrawal{}
