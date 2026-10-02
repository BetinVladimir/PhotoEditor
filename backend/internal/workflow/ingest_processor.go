package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"time"
)

var ErrIngestLeaseLost = errors.New("ingest build lease lost")

type IngestLeases interface {
	Claim(context.Context) (*IngestBuildLease, error)
	Renew(context.Context, IngestBuildLease) (bool, error)
	Release(context.Context, IngestBuildLease) (bool, error)
}
type IngestRunAdvancer interface {
	Advance(context.Context, uuid.UUID, uuid.UUID) (bool, error)
}
type IngestProcessor struct {
	Leases                                       IngestLeases
	Runner                                       IngestRunAdvancer
	HeartbeatInterval, RenewTimeout, WorkTimeout time.Duration
}

// The lease limits duplicate work; correctness relies on immutable plans,
// document reservation tokens and phase-checked SQL mutations. Expiry may overlap
// an in-flight operation; this is not an exactly-once transaction around object IO.
func (p IngestProcessor) ProcessOne(ctx context.Context) (bool, error) {
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
	if p.Leases == nil || p.Runner == nil || interval <= 0 || interval > 10*time.Second || timeout <= 0 || timeout > 5*time.Second || budget <= 0 || budget > 5*time.Minute {
		return false, errors.New("invalid ingest processor")
	}
	claim, done := context.WithTimeout(ctx, 5*time.Second)
	lease, err := p.Leases.Claim(claim)
	done()
	if err != nil || lease == nil {
		return false, err
	}
	defer func() {
		release, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, _ = p.Leases.Release(release, *lease)
	}()
	work, cancelBudget := context.WithTimeout(ctx, budget)
	defer cancelBudget()
	work, cancelWork := context.WithCancelCause(work)
	defer cancelWork(nil)
	renew := func(c context.Context) error {
		check, cancel := context.WithTimeout(c, timeout)
		defer cancel()
		ok, e := p.Leases.Renew(check, *lease)
		if e != nil {
			return e
		}
		if !ok {
			return ErrIngestLeaseLost
		}
		return nil
	}
	if err = renew(work); err != nil {
		return false, err
	}
	heartbeat, stop := context.WithCancel(work)
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		tick := time.NewTicker(interval)
		defer tick.Stop()
		for {
			select {
			case <-heartbeat.Done():
				return
			case <-tick.C:
				e := renew(heartbeat)
				if heartbeat.Err() != nil {
					return
				}
				if e != nil {
					cancelWork(e)
					return
				}
			}
		}
	}()
	changed, err := p.Runner.Advance(work, lease.TenantID, lease.RunID)
	stop()
	<-joined
	if cause := context.Cause(work); cause != nil {
		return false, cause
	}
	return changed, err
}
