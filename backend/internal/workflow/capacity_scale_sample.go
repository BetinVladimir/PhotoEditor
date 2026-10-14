package workflow

import (
	"errors"
	"math"
	"time"
)

// ScaleSampleFromHeads derives eligible wait from the scheduler's complete
// qualified head snapshot. PendingBacklog includes ready tasks that could not
// enter that snapshot, so an unavailable model cannot be mistaken for idle.
// Utilization and active reservations come from aggregate monitoring only.
func ScaleSampleFromHeads(now time.Time, heads CapacityHeadSnapshot,
	reservedUtilization float64, activeReservations int64, pendingBacklog bool) (ScaleSample, error) {
	if now.IsZero() || math.IsNaN(reservedUtilization) || math.IsInf(reservedUtilization, 0) ||
		reservedUtilization < 0 || reservedUtilization > 1 || activeReservations < 0 {
		return ScaleSample{}, errors.New("invalid capacity scale aggregates")
	}
	oldestWait := time.Duration(0)
	for _, head := range heads.Heads {
		if head.CreatedAt.IsZero() || head.CreatedAt.After(now) {
			return ScaleSample{}, errors.New("invalid eligible head time")
		}
		wait := now.Sub(head.CreatedAt)
		if wait > oldestWait {
			oldestWait = wait
		}
	}
	return ScaleSample{At: now, OldestEligibleWait: oldestWait,
		ReservedUtilization: reservedUtilization,
		Idle:                len(heads.Heads) == 0 && !pendingBacklog && activeReservations == 0 && reservedUtilization == 0}, nil
}
