package workflow

import (
	"math"
	"testing"
	"time"
)

func TestScaleSampleFromHeadsUsesOldestEligibleWait(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	heads := CapacityHeadSnapshot{Heads: []CapacityCandidate{
		{CreatedAt: now.Add(-10 * time.Second)},
		{CreatedAt: now.Add(-42 * time.Second)},
	}}
	sample, err := ScaleSampleFromHeads(now, heads, .25, 1, true)
	if err != nil || sample.OldestEligibleWait != 42*time.Second || sample.Idle || sample.ReservedUtilization != .25 {
		t.Fatal(sample, err)
	}
}

func TestScaleSampleFromHeadsUnavailableBacklogIsNotIdle(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	sample, err := ScaleSampleFromHeads(now, CapacityHeadSnapshot{}, 0, 0, true)
	if err != nil || sample.Idle || sample.OldestEligibleWait != 0 {
		t.Fatal(sample, err)
	}
	sample, err = ScaleSampleFromHeads(now, CapacityHeadSnapshot{}, 0, 0, false)
	if err != nil || !sample.Idle {
		t.Fatal(sample, err)
	}
}

func TestScaleSampleFromHeadsRejectsMalformedAggregates(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	if _, err := ScaleSampleFromHeads(now, CapacityHeadSnapshot{}, math.NaN(), 0, false); err == nil {
		t.Fatal("NaN utilization accepted")
	}
	if _, err := ScaleSampleFromHeads(now, CapacityHeadSnapshot{Heads: []CapacityCandidate{
		{CreatedAt: now.Add(time.Second)},
	}}, 0, 0, false); err == nil {
		t.Fatal("future eligible head accepted")
	}
}
