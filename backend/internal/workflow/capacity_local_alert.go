package workflow

import (
	"context"
	"errors"
	"time"
)

type LocalScaleObservationSource interface {
	ObserveScaleFromLedger(context.Context, string) (ScaleSample, CapacityPoolUsage, error)
}

type CapacityAlert struct {
	Pool   string
	Reason string
	Active bool
}

// LocalCapacityMonitor emits only state transitions. It has no provisioner and
// cannot create or stop a node in the single-node Metal/RTX deployment mode.
type LocalCapacityMonitor struct {
	source   LocalScaleObservationSource
	pools    []string
	trackers map[string]*ScaleTracker
	active   map[string]string
}

func NewLocalCapacityMonitor(source LocalScaleObservationSource, pools []string) (*LocalCapacityMonitor, error) {
	if source == nil || len(pools) == 0 || len(pools) > 3 {
		return nil, errors.New("invalid local capacity monitor")
	}
	monitor := &LocalCapacityMonitor{source: source, pools: append([]string(nil), pools...),
		trackers: make(map[string]*ScaleTracker, len(pools)), active: make(map[string]string)}
	for _, pool := range monitor.pools {
		if monitor.trackers[pool] != nil {
			return nil, errors.New("duplicate local capacity pool")
		}
		tracker, err := NewScaleTracker(pool)
		if err != nil {
			return nil, err
		}
		monitor.trackers[pool] = tracker
	}
	return monitor, nil
}

// Tick must run serially with CapacityRuntime.Dispatch on its leader session.
// A failed pool observation cannot authorize scaling and is retried next tick.
func (m *LocalCapacityMonitor) Tick(ctx context.Context) ([]CapacityAlert, error) {
	if m == nil {
		return nil, errors.New("missing local capacity monitor")
	}
	alerts := make([]CapacityAlert, 0)
	var failures []error
	for _, pool := range m.pools {
		sample, _, err := m.source.ObserveScaleFromLedger(ctx, pool)
		if err == nil {
			err = m.trackers[pool].Observe(sample)
		}
		var decision ScaleDecision
		if err == nil {
			decision, err = m.trackers[pool].Decision(sample.At, 1, 1, 1, true, nil, nil)
		}
		if err != nil {
			failures = append(failures, err)
			continue
		}
		previous := m.active[pool]
		if decision.Alert {
			if previous != decision.Reason {
				alerts = append(alerts, CapacityAlert{Pool: pool, Reason: decision.Reason, Active: true})
			}
			m.active[pool] = decision.Reason
		} else if previous != "" && sample.OldestEligibleWait <= 30*time.Second && sample.ReservedUtilization <= .75 {
			alerts = append(alerts, CapacityAlert{Pool: pool, Reason: previous, Active: false})
			delete(m.active, pool)
		}
	}
	return alerts, errors.Join(failures...)
}
