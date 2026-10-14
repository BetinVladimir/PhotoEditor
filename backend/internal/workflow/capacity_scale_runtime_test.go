package workflow

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

func TestCapacityRuntimeObserveScaleRefusesClosedLeader(t *testing.T) {
	runtime := &CapacityRuntime{}
	if _, err := runtime.ObserveScale(context.Background(), "gpu", CapacityScaleMonitoring{}); !errors.Is(err, ErrCapacityLeaderUnavailable) {
		t.Fatal(err)
	}
}

func TestCapacityRuntimeObserveScaleRejectsInvalidMonitoringBeforeQuery(t *testing.T) {
	runtime := &CapacityRuntime{leader: &CapacityLeader{},
		dispatchers: []*CapacityDispatcher{{}, {}, {}}}
	monitoring := CapacityScaleMonitoring{At: time.Now(), ReservedUtilization: math.NaN()}
	if _, err := runtime.ObserveScale(context.Background(), "gpu", monitoring); err == nil {
		t.Fatal("invalid utilization reached leader")
	}
	monitoring.ReservedUtilization = 0
	if _, err := runtime.ObserveScale(context.Background(), "other", monitoring); err == nil {
		t.Fatal("invalid pool reached leader")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runtime.ObserveScale(ctx, "gpu", monitoring); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
