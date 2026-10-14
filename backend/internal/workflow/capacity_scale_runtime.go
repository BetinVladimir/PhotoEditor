package workflow

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrStaleCapacityScaleMonitoring = errors.New("stale capacity scale monitoring")

// CapacityScaleMonitoring contains aggregate measurements from the installed
// monitor. It carries no tenant, image, signer key, or task payload data.
type CapacityScaleMonitoring struct {
	At                  time.Time
	ReservedUtilization float64
	ActiveReservations  int64
}

// ObserveScale runs on the serialized capacity leader loop. Queue eligibility,
// backlog, policy, and DB time are read within one scheduler transaction;
// stale external utilization is rejected instead of silently scaling on it.
func (r *CapacityRuntime) ObserveScale(ctx context.Context, pool string, monitoring CapacityScaleMonitoring) (ScaleSample, error) {
	if r == nil || r.leader == nil || len(r.dispatchers) != 3 {
		return ScaleSample{}, ErrCapacityLeaderUnavailable
	}
	if (pool != "cpu" && pool != "gpu" && pool != "signer") ||
		monitoring.At.IsZero() || monitoring.ActiveReservations < 0 ||
		math.IsNaN(monitoring.ReservedUtilization) || math.IsInf(monitoring.ReservedUtilization, 0) ||
		monitoring.ReservedUtilization < 0 || monitoring.ReservedUtilization > 1 {
		return ScaleSample{}, errors.New("invalid capacity scale monitoring")
	}
	if err := ctx.Err(); err != nil {
		return ScaleSample{}, err
	}
	var sample ScaleSample
	err := r.leader.Transaction(ctx, func(tx pgx.Tx) error {
		if err := CheckCapacitySchedulerContract(ctx, tx); err != nil {
			return err
		}
		policy, err := ReadCapacityQueuePolicy(ctx, tx, r.pin)
		if err != nil {
			return err
		}
		var now time.Time
		if err := tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
			return err
		}
		if monitoring.At.After(now) || now.Sub(monitoring.At) > 30*time.Second {
			return ErrStaleCapacityScaleMonitoring
		}
		heads, err := ReadCapacityHeadSnapshot(ctx, tx, pool, now, policy)
		if err != nil {
			return err
		}
		backlog, err := ReadCapacityPoolPendingBacklog(ctx, tx, pool)
		if err != nil {
			return err
		}
		sample, err = ScaleSampleFromHeads(now, heads, monitoring.ReservedUtilization,
			monitoring.ActiveReservations, backlog)
		return err
	})
	if err != nil {
		return ScaleSample{}, err
	}
	return sample, nil
}
