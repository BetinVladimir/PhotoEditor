package workflow

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ObserveScaleFromLedger reads the installed policy, reservation utilization,
// qualified queue heads and idle veto on the serialized capacity leader.
// It is an aggregate operator input; no tenant or image data leaves this API.
func (r *CapacityRuntime) ObserveScaleFromLedger(ctx context.Context, pool string) (ScaleSample, CapacityPoolUsage, error) {
	if r == nil || r.leader == nil || len(r.dispatchers) != 3 {
		return ScaleSample{}, CapacityPoolUsage{}, ErrCapacityLeaderUnavailable
	}
	if pool != "cpu" && pool != "gpu" && pool != "signer" {
		return ScaleSample{}, CapacityPoolUsage{}, errors.New("invalid capacity scale pool")
	}
	if err := ctx.Err(); err != nil {
		return ScaleSample{}, CapacityPoolUsage{}, err
	}
	var sample ScaleSample
	var usage CapacityPoolUsage
	err := r.leader.Transaction(ctx, func(tx pgx.Tx) error {
		if err := CheckCapacitySchedulerContract(ctx, tx); err != nil {
			return err
		}
		policy, err := ReadCapacityQueuePolicy(ctx, tx, r.pin)
		if err != nil {
			return err
		}
		usage, err = ReadCapacityPoolUsage(ctx, tx, pool, r.pin)
		if err != nil {
			return err
		}
		var now time.Time
		if err := tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
			return err
		}
		if usage.Monitoring.At.After(now) || now.Sub(usage.Monitoring.At) > 30*time.Second {
			return errors.New("stale capacity ledger observation")
		}
		heads, err := ReadCapacityHeadSnapshot(ctx, tx, pool, now, policy)
		if err != nil {
			return err
		}
		backlog, err := ReadCapacityPoolPendingBacklog(ctx, tx, pool)
		if err != nil {
			return err
		}
		sample, err = ScaleSampleFromHeads(now, heads, usage.Monitoring.ReservedUtilization,
			usage.Monitoring.ActiveReservations, backlog)
		return err
	})
	if err != nil {
		return ScaleSample{}, CapacityPoolUsage{}, err
	}
	return sample, usage, nil
}
