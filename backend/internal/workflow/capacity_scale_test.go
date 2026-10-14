package workflow

import (
	"testing"
	"time"
)

func TestComputeScaleDecisionThresholdsAndCooldowns(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	base := ScaleWindow{Pool: "gpu", Now: now, CurrentNodes: 2, MinimumNodes: 1, MaximumNodes: 4}
	wait := now.Add(-179 * time.Second)
	base.WaitAbove30Since = &wait
	if result, err := ComputeScaleDecision(base); err != nil || result.Delta != 0 {
		t.Fatal(result, err)
	}
	wait = now.Add(-180 * time.Second)
	if result, err := ComputeScaleDecision(base); err != nil || result.Delta != 1 || result.Reason != "eligible_wait" {
		t.Fatal(result, err)
	}
	lastUp := now.Add(-59 * time.Second)
	base.LastUp = &lastUp
	if result, err := ComputeScaleDecision(base); err != nil || result.Delta != 0 {
		t.Fatal(result, err)
	}
	base.WaitAbove30Since = nil
	base.LastUp = nil
	util := now.Add(-300 * time.Second)
	base.UtilAbove75Since = &util
	if result, err := ComputeScaleDecision(base); err != nil || result.Delta != 1 || result.Reason != "reserved_utilization" {
		t.Fatal(result, err)
	}
	base.UtilAbove75Since = nil
	idle := now.Add(-900 * time.Second)
	base.IdleSince = &idle
	if result, err := ComputeScaleDecision(base); err != nil || result.Delta != -1 {
		t.Fatal(result, err)
	}
	lastDown := now.Add(-299 * time.Second)
	base.LastDown = &lastDown
	if result, err := ComputeScaleDecision(base); err != nil || result.Delta != 0 {
		t.Fatal(result, err)
	}
}

func TestComputeScaleDecisionLocalAlertAndInvalidEvidence(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	wait := now.Add(-180 * time.Second)
	local := ScaleWindow{Pool: "gpu", Now: now, WaitAbove30Since: &wait,
		CurrentNodes: 1, MinimumNodes: 1, MaximumNodes: 1, LocalOnly: true}
	if result, err := ComputeScaleDecision(local); err != nil || result.Delta != 0 || !result.Alert {
		t.Fatal(result, err)
	}
	idle := now.Add(-900 * time.Second)
	local.IdleSince = &idle
	if _, err := ComputeScaleDecision(local); err == nil {
		t.Fatal("contradictory observations accepted")
	}
	local.IdleSince = nil
	future := now.Add(time.Second)
	local.WaitAbove30Since = &future
	if _, err := ComputeScaleDecision(local); err == nil {
		t.Fatal("future observation accepted")
	}
}
