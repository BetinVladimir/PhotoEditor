package workflow

import (
	"context"
	"errors"
	"time"
)

var ErrExpansionLeaseLost = errors.New("expansion lease lost")

type ExpansionLeases interface {
	Claim(context.Context) (*DefectExpansionLease, error)
	Renew(context.Context, DefectExpansionLease) (bool, error)
	Release(context.Context, DefectExpansionLease) (bool, error)
	Materialize(context.Context, DefectExpansionLease, MaterializedDefectBranch, *PreparedDefectTask) (bool, error)
}
type ExpansionBuild func(context.Context, DefectExpansionLease) (MaterializedDefectBranch, *PreparedDefectTask, error)

type ExpansionProcessor struct {
	Leases                                       ExpansionLeases
	Build                                        ExpansionBuild
	HeartbeatInterval, RenewTimeout, WorkTimeout time.Duration
}

// ProcessOne keeps a claimed expansion alive only during cancellable build/IO.
// Heartbeat stops and joins before final SQL materialization, avoiding a false
// lease-loss cancellation after SQL has cleared the lease on successful commit.
func (p ExpansionProcessor) ProcessOne(ctx context.Context) (bool, error) {
	interval, timeout, budget := p.HeartbeatInterval, p.RenewTimeout, p.WorkTimeout
	if interval == 0 {
		interval = 10 * time.Second
	}
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	if budget == 0 {
		budget = 5 * time.Minute
	}
	if p.Leases == nil || p.Build == nil || interval <= 0 || interval > 10*time.Second || timeout <= 0 || timeout > 5*time.Second || budget <= 0 || budget > 5*time.Minute {
		return false, errors.New("invalid expansion processor")
	}
	claimCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	lease, err := p.Leases.Claim(claimCtx)
	cancel()
	if err != nil || lease == nil {
		return false, err
	}
	committed := false
	defer func() {
		if !committed {
			release, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer done()
			_, _ = p.Leases.Release(release, *lease)
		}
	}()
	work, cancelBudget := context.WithTimeout(ctx, budget)
	defer cancelBudget()
	work, cancelWork := context.WithCancelCause(work)
	defer cancelWork(nil)
	renew := func(c context.Context) error {
		check, done := context.WithTimeout(c, timeout)
		defer done()
		ok, err := p.Leases.Renew(check, *lease)
		if err != nil {
			return err
		}
		if !ok {
			return ErrExpansionLeaseLost
		}
		return nil
	}
	if err = renew(work); err != nil {
		return false, err
	}
	heartbeat, stopHeartbeat := context.WithCancel(work)
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeat.Done():
				return
			case <-ticker.C:
				err := renew(heartbeat)
				if heartbeat.Err() != nil {
					return
				}
				if err != nil {
					cancelWork(err)
					return
				}
			}
		}
	}()
	branch, task, buildErr := p.Build(work, *lease)
	stopHeartbeat()
	<-heartbeatDone
	if cause := context.Cause(work); cause != nil {
		return false, cause
	}
	if buildErr != nil {
		return false, buildErr
	}
	finalize, done := context.WithTimeout(work, 5*time.Second)
	defer done()
	committed, err = p.Leases.Materialize(finalize, *lease, branch, task)
	if err != nil {
		return false, err
	}
	if !committed {
		return false, ErrExpansionLeaseLost
	}
	return true, nil
}
