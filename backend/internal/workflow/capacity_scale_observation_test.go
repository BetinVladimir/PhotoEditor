package workflow

import (
	"testing"
	"time"
)

func TestScaleTrackerRequiresContinuousEligibleWait(t *testing.T) {
	tracker, err := NewScaleTracker("gpu")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	for i := 0; i <= 6; i++ {
		if err := tracker.Observe(ScaleSample{At: start.Add(time.Duration(i) * 30 * time.Second),
			OldestEligibleWait: 31 * time.Second}); err != nil {
			t.Fatal(err)
		}
	}
	if decision, err := tracker.Decision(start.Add(180*time.Second), 1, 1, 3, false, nil, nil); err != nil || decision.Delta != 1 {
		t.Fatal(decision, err)
	}
	// The missing interval resets continuity even though the next wait is high.
	if err := tracker.Observe(ScaleSample{At: start.Add(220 * time.Second),
		OldestEligibleWait: 220 * time.Second}); err != nil {
		t.Fatal(err)
	}
	if decision, err := tracker.Decision(start.Add(220*time.Second), 1, 1, 3, false, nil, nil); err != nil || decision.Delta != 0 {
		t.Fatal(decision, err)
	}
	// Unqualified backlog is excluded by the source and clears the wait timer.
	if err := tracker.Observe(ScaleSample{At: start.Add(240 * time.Second)}); err != nil {
		t.Fatal(err)
	}
	if decision, err := tracker.Decision(start.Add(240*time.Second), 1, 1, 3, false, nil, nil); err != nil || decision.Delta != 0 {
		t.Fatal(decision, err)
	}
}

func TestScaleTrackerLocalAlertAndMalformedSamples(t *testing.T) {
	tracker, err := NewScaleTracker("gpu")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	for i := 0; i <= 10; i++ {
		if err := tracker.Observe(ScaleSample{At: start.Add(time.Duration(i) * 30 * time.Second),
			ReservedUtilization: .8}); err != nil {
			t.Fatal(err)
		}
	}
	if decision, err := tracker.Decision(start.Add(300*time.Second), 1, 1, 1, true, nil, nil); err != nil || !decision.Alert || decision.Delta != 0 {
		t.Fatal(decision, err)
	}
	if _, err := tracker.Decision(start.Add(331*time.Second), 1, 1, 1, true, nil, nil); err == nil {
		t.Fatal("stale observations accepted")
	}
	if err := tracker.Observe(ScaleSample{At: start.Add(330 * time.Second), Idle: true,
		ReservedUtilization: .1}); err == nil {
		t.Fatal("contradictory idle sample accepted")
	}
	if err := tracker.Observe(ScaleSample{At: start.Add(300 * time.Second),
		ReservedUtilization: .8}); err == nil {
		t.Fatal("duplicate timestamp accepted")
	}
}
