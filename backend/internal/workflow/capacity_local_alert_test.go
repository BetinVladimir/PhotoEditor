package workflow

import (
	"context"
	"testing"
	"time"
)

type localScaleSource struct{ sample ScaleSample }

func (s *localScaleSource) ObserveScaleFromLedger(_ context.Context, _ string) (ScaleSample, CapacityPoolUsage, error) {
	return s.sample, CapacityPoolUsage{}, nil
}

func TestLocalCapacityMonitorAlertTransitions(t *testing.T) {
	source := &localScaleSource{}
	monitor, err := NewLocalCapacityMonitor(source, []string{"gpu"})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	for i := 0; i <= 18; i++ {
		source.sample = ScaleSample{At: start.Add(time.Duration(i) * 10 * time.Second),
			OldestEligibleWait: 31 * time.Second}
		alerts, err := monitor.Tick(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if i < 18 && len(alerts) != 0 {
			t.Fatal("early alert", alerts)
		}
		if i == 18 && (len(alerts) != 1 || !alerts[0].Active || alerts[0].Reason != "eligible_wait") {
			t.Fatal("threshold alert missing", alerts)
		}
	}
	source.sample.At = start.Add(190 * time.Second)
	if alerts, err := monitor.Tick(context.Background()); err != nil || len(alerts) != 0 {
		t.Fatal("duplicate alert", alerts, err)
	}
	source.sample.At = start.Add(200 * time.Second)
	source.sample.OldestEligibleWait = 0
	alerts, err := monitor.Tick(context.Background())
	if err != nil || len(alerts) != 1 || alerts[0].Active {
		t.Fatal("alert did not clear", alerts, err)
	}
}

func TestLocalCapacityMonitorRejectsDuplicatePool(t *testing.T) {
	if _, err := NewLocalCapacityMonitor(&localScaleSource{}, []string{"gpu", "gpu"}); err == nil {
		t.Fatal("duplicate pool accepted")
	}
}

func TestLocalCapacityMonitorTelemetryGapDoesNotClearPressure(t *testing.T) {
	source := &localScaleSource{}
	monitor, err := NewLocalCapacityMonitor(source, []string{"gpu"})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	for i := 0; i <= 18; i++ {
		source.sample = ScaleSample{At: start.Add(time.Duration(i) * 10 * time.Second),
			OldestEligibleWait: 31 * time.Second}
		if _, err := monitor.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	source.sample.At = start.Add(250 * time.Second)
	alerts, err := monitor.Tick(context.Background())
	if err != nil || len(alerts) != 0 {
		t.Fatal("telemetry gap falsely cleared alert", alerts, err)
	}
	source.sample.At = start.Add(260 * time.Second)
	source.sample.OldestEligibleWait = 0
	alerts, err = monitor.Tick(context.Background())
	if err != nil || len(alerts) != 1 || alerts[0].Active {
		t.Fatal("true recovery did not clear alert", alerts, err)
	}
}
