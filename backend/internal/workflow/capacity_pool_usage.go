package workflow

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type CapacityUsageQueries interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type CapacityPoolUsage struct {
	Monitoring CapacityScaleMonitoring
	Nodes      int64
	Capacity   int64
}

// ReadCapacityPoolUsage uses the existing reservation ledger and installed
// concurrency policy. Overcommit is clamped to utilization 1 for alerting;
// missing capacity with live reservations remains an error.
func ReadCapacityPoolUsage(ctx context.Context, db CapacityUsageQueries, pool, policySHA string) (CapacityPoolUsage, error) {
	if db == nil || (pool != "cpu" && pool != "gpu" && pool != "signer") ||
		!capacityPolicySHA.MatchString(policySHA) {
		return CapacityPoolUsage{}, errors.New("invalid capacity usage source")
	}
	if err := ctx.Err(); err != nil {
		return CapacityPoolUsage{}, err
	}
	var used, capacity, nodes int64
	var at time.Time
	if err := db.QueryRow(ctx,
		"SELECT used_reservations,available_slots,node_count,observed_at FROM photo.capacity_pool_resource_usage($1,$2)",
		pool, policySHA).Scan(&used, &capacity, &nodes, &at); err != nil {
		return CapacityPoolUsage{}, err
	}
	if used < 0 || capacity < 0 || nodes < 0 || at.IsZero() ||
		(nodes == 0) != (capacity == 0) || (capacity == 0 && used != 0) {
		return CapacityPoolUsage{}, errors.New("invalid capacity usage aggregate")
	}
	utilization := float64(0)
	if capacity > 0 {
		utilization = float64(used) / float64(capacity)
		if utilization > 1 {
			utilization = 1
		}
	}
	return CapacityPoolUsage{Monitoring: CapacityScaleMonitoring{At: at,
		ReservedUtilization: utilization, ActiveReservations: used},
		Nodes: nodes, Capacity: capacity}, nil
}
