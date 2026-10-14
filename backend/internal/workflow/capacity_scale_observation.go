package workflow

import (
	"errors"
	"math"
	"time"
)

// ScaleSample contains aggregate monitoring data only. OldestEligibleWait is
// zero when no currently qualified task is waiting; unavailable-model backlog
// must not be included by the monitoring source.
type ScaleSample struct {
	At                  time.Time
	OldestEligibleWait  time.Duration
	ReservedUtilization float64
	Idle                bool
}

// ScaleTracker owns continuous threshold starts for one pool. A telemetry gap
// over 30 seconds resets all starts, so stale observations cannot trigger scale.
type ScaleTracker struct {
	pool      string
	last      time.Time
	waitSince *time.Time
	utilSince *time.Time
	idleSince *time.Time
}

func NewScaleTracker(pool string) (*ScaleTracker, error) {
	if pool != "cpu" && pool != "gpu" && pool != "signer" {
		return nil, errors.New("invalid scale pool")
	}
	return &ScaleTracker{pool: pool}, nil
}

func (t *ScaleTracker) Observe(sample ScaleSample) error {
	if t == nil || sample.At.IsZero() || sample.OldestEligibleWait < 0 ||
		math.IsNaN(sample.ReservedUtilization) || math.IsInf(sample.ReservedUtilization, 0) ||
		sample.ReservedUtilization < 0 || sample.ReservedUtilization > 1 ||
		sample.Idle && (sample.OldestEligibleWait > 0 || sample.ReservedUtilization > 0) ||
		!t.last.IsZero() && !sample.At.After(t.last) {
		return errors.New("invalid scale sample")
	}
	if !t.last.IsZero() && sample.At.Sub(t.last) > 30*time.Second {
		t.waitSince, t.utilSince, t.idleSince = nil, nil, nil
	}
	advance := func(since **time.Time, active bool) {
		if !active {
			*since = nil
		} else if *since == nil {
			at := sample.At
			*since = &at
		}
	}
	advance(&t.waitSince, sample.OldestEligibleWait > 30*time.Second)
	advance(&t.utilSince, sample.ReservedUtilization > .75)
	advance(&t.idleSince, sample.Idle)
	t.last = sample.At
	return nil
}

// Decision adds operator-owned node bounds and persisted last-action times.
// The tracker never provisions, stops or drains a node.
func (t *ScaleTracker) Decision(now time.Time, current, minimum, maximum int, localOnly bool,
	lastUp, lastDown *time.Time) (ScaleDecision, error) {
	if t == nil || t.last.IsZero() {
		return ScaleDecision{}, errors.New("missing scale observation")
	}
	if now.Before(t.last) || now.Sub(t.last) > 30*time.Second {
		return ScaleDecision{}, errors.New("stale scale observation")
	}
	return ComputeScaleDecision(ScaleWindow{Pool: t.pool, Now: now,
		WaitAbove30Since: t.waitSince, UtilAbove75Since: t.utilSince,
		IdleSince: t.idleSince, LastUp: lastUp, LastDown: lastDown,
		CurrentNodes: current, MinimumNodes: minimum, MaximumNodes: maximum,
		LocalOnly: localOnly})
}
