package workflow

import (
	"context"
	"errors"
	"sync/atomic"
	"time"
)

type ExpansionJob interface {
	ProcessOne(context.Context) (bool, error)
}
type ExpansionLoopState struct {
	Ready                  atomic.Bool
	Materialized, Failures atomic.Uint64
}

// RunExpansionLoop runs at most one expansion at a time; dispatch/recovery run
// independently so object IO cannot delay worker lease recovery. ProcessOne owns
// its heartbeat and bounded work timeout. Errors back off without leaking data.
func RunExpansionLoop(ctx context.Context, job ExpansionJob, state *ExpansionLoopState, idle time.Duration) error {
	if job == nil || state == nil || idle <= 0 {
		return errors.New("invalid expansion loop")
	}
	defer state.Ready.Store(false)
	for ctx.Err() == nil {
		done, err := job.ProcessOne(ctx)
		if ctx.Err() != nil {
			return nil
		}
		state.Ready.Store(err == nil)
		delay := idle
		if err != nil {
			state.Failures.Add(1)
		} else if done {
			state.Materialized.Add(1)
			delay = 100 * time.Millisecond
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
	return nil
}
