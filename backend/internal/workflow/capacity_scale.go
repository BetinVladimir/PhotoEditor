package workflow

import (
	"errors"
	"time"
)

// ScaleWindow carries continuous operator observations. A Since value is set
// only while that condition has held without interruption; a sample below its
// threshold must clear it. An unavailable-model backlog never sets WaitSince.
type ScaleWindow struct {
	Pool             string
	Now              time.Time
	WaitAbove30Since *time.Time
	UtilAbove75Since *time.Time
	IdleSince        *time.Time
	LastUp           *time.Time
	LastDown         *time.Time
	CurrentNodes     int
	MinimumNodes     int
	MaximumNodes     int
	LocalOnly        bool
}

type ScaleDecision struct {
	Delta  int
	Alert  bool
	Reason string
}

// ComputeScaleDecision returns at most one node per policy interval. It never
// provisions a node: an operator adapter must separately enforce credentials,
// policy pin and idempotency before applying a nonzero Delta.
func ComputeScaleDecision(window ScaleWindow) (ScaleDecision, error) {
	zero := ScaleDecision{}
	if window.Pool != "cpu" && window.Pool != "gpu" && window.Pool != "signer" {
		return zero, errors.New("invalid scale pool")
	}
	if window.Now.IsZero() || window.MinimumNodes < 1 || window.MaximumNodes < window.MinimumNodes ||
		window.CurrentNodes < window.MinimumNodes || window.CurrentNodes > window.MaximumNodes ||
		window.LocalOnly && (window.MinimumNodes != 1 || window.MaximumNodes != 1) {
		return zero, errors.New("invalid scale bounds")
	}
	for _, since := range []*time.Time{window.WaitAbove30Since, window.UtilAbove75Since,
		window.IdleSince, window.LastUp, window.LastDown} {
		if since != nil && (since.IsZero() || since.After(window.Now)) {
			return zero, errors.New("invalid scale observation time")
		}
	}
	waiting := window.WaitAbove30Since != nil && window.Now.Sub(*window.WaitAbove30Since) >= 180*time.Second
	utilized := window.UtilAbove75Since != nil && window.Now.Sub(*window.UtilAbove75Since) >= 300*time.Second
	idle := window.IdleSince != nil && window.Now.Sub(*window.IdleSince) >= 900*time.Second
	if window.IdleSince != nil && (window.WaitAbove30Since != nil || window.UtilAbove75Since != nil) {
		return zero, errors.New("contradictory scale observations")
	}
	if waiting || utilized {
		reason := "eligible_wait"
		if !waiting {
			reason = "reserved_utilization"
		}
		if window.LocalOnly {
			return ScaleDecision{Alert: true, Reason: reason}, nil
		}
		if window.CurrentNodes < window.MaximumNodes &&
			(window.LastUp == nil || window.Now.Sub(*window.LastUp) >= 60*time.Second) {
			return ScaleDecision{Delta: 1, Reason: reason}, nil
		}
		return zero, nil
	}
	if idle && !window.LocalOnly && window.CurrentNodes > window.MinimumNodes &&
		(window.LastDown == nil || window.Now.Sub(*window.LastDown) >= 300*time.Second) {
		return ScaleDecision{Delta: -1, Reason: "idle"}, nil
	}
	return zero, nil
}
