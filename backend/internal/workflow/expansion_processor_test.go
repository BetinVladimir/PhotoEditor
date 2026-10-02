package workflow

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type processorLeases struct {
	ExpansionLeases
	renew                               func(context.Context, int64) (bool, error)
	renews, active, finalizes, releases atomic.Int64
	finalizedWithHeartbeat              atomic.Bool
}

func (p *processorLeases) Claim(context.Context) (*DefectExpansionLease, error) {
	return &DefectExpansionLease{}, nil
}
func (p *processorLeases) Renew(ctx context.Context, _ DefectExpansionLease) (bool, error) {
	p.active.Add(1)
	defer p.active.Add(-1)
	return p.renew(ctx, p.renews.Add(1))
}
func (p *processorLeases) Release(context.Context, DefectExpansionLease) (bool, error) {
	p.releases.Add(1)
	return true, nil
}
func (p *processorLeases) Materialize(context.Context, DefectExpansionLease, MaterializedDefectBranch, *PreparedDefectTask) (bool, error) {
	p.finalizes.Add(1)
	p.finalizedWithHeartbeat.Store(p.active.Load() != 0)
	return true, nil
}
func TestExpansionHeartbeatLossCancelsBuild(t *testing.T) {
	for _, mode := range []string{"lost", "error"} {
		t.Run(mode, func(t *testing.T) {
			failure := errors.New("database failed")
			leases := &processorLeases{renew: func(ctx context.Context, n int64) (bool, error) {
				if n == 1 {
					return true, nil
				}
				if mode == "error" {
					return false, failure
				}
				return false, nil
			}}
			build := func(ctx context.Context, _ DefectExpansionLease) (MaterializedDefectBranch, *PreparedDefectTask, error) {
				<-ctx.Done()
				return MaterializedDefectBranch{}, nil, ctx.Err()
			}
			p := ExpansionProcessor{Leases: leases, Build: build, HeartbeatInterval: time.Millisecond, RenewTimeout: time.Second, WorkTimeout: time.Second}
			done, err := p.ProcessOne(context.Background())
			want := ErrExpansionLeaseLost
			if mode == "error" {
				want = failure
			}
			if done || !errors.Is(err, want) || leases.finalizes.Load() != 0 || leases.releases.Load() != 1 {
				t.Fatal("work committed after lease loss", err)
			}
		})
	}
}
func TestExpansionHeartbeatJoinedBeforeCommit(t *testing.T) {
	entered := make(chan struct{})
	leases := &processorLeases{renew: func(ctx context.Context, n int64) (bool, error) {
		if n == 1 {
			return true, nil
		}
		close(entered)
		<-ctx.Done()
		return false, ctx.Err()
	}}
	build := func(ctx context.Context, _ DefectExpansionLease) (MaterializedDefectBranch, *PreparedDefectTask, error) {
		select {
		case <-entered:
			return MaterializedDefectBranch{Route: "skip"}, nil, nil
		case <-ctx.Done():
			return MaterializedDefectBranch{}, nil, ctx.Err()
		}
	}
	p := ExpansionProcessor{Leases: leases, Build: build, HeartbeatInterval: time.Millisecond, RenewTimeout: time.Second, WorkTimeout: time.Second}
	done, err := p.ProcessOne(context.Background())
	if err != nil || !done || leases.finalizes.Load() != 1 || leases.finalizedWithHeartbeat.Load() || leases.releases.Load() != 0 {
		t.Fatal("heartbeat raced final commit", err)
	}
}
func TestExpansionWorkTimeoutAndInitialLostLease(t *testing.T) {
	for _, initial := range []bool{true, false} {
		leases := &processorLeases{renew: func(context.Context, int64) (bool, error) { return initial, nil }}
		called := false
		build := func(ctx context.Context, _ DefectExpansionLease) (MaterializedDefectBranch, *PreparedDefectTask, error) {
			called = true
			<-ctx.Done()
			return MaterializedDefectBranch{}, nil, ctx.Err()
		}
		p := ExpansionProcessor{Leases: leases, Build: build, HeartbeatInterval: time.Millisecond, WorkTimeout: 10 * time.Millisecond}
		done, err := p.ProcessOne(context.Background())
		if done || err == nil || leases.finalizes.Load() != 0 || called != initial || leases.releases.Load() != 1 {
			t.Fatal("invalid timeout/initial lease handling", err)
		}
	}
}
