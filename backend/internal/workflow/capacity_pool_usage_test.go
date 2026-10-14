package workflow

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type capacityUsageRow struct {
	used, capacity, nodes int64
	at                    time.Time
	err                   error
}

func (r capacityUsageRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != 4 {
		return errors.New("invalid usage scan")
	}
	*dest[0].(*int64), *dest[1].(*int64), *dest[2].(*int64), *dest[3].(*time.Time) =
		r.used, r.capacity, r.nodes, r.at
	return nil
}

type capacityUsageQuery struct {
	row   capacityUsageRow
	calls int
}

func (q *capacityUsageQuery) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	q.calls++
	return q.row
}

func TestReadCapacityPoolUsageAndOvercommit(t *testing.T) {
	at := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	q := &capacityUsageQuery{row: capacityUsageRow{used: 3, capacity: 2, nodes: 1, at: at}}
	usage, err := ReadCapacityPoolUsage(context.Background(), q, "gpu", strings.Repeat("a", 64))
	if err != nil || usage.Monitoring.ReservedUtilization != 1 ||
		usage.Monitoring.ActiveReservations != 3 || usage.Nodes != 1 || q.calls != 1 {
		t.Fatal(usage, err)
	}
	q.row = capacityUsageRow{at: at}
	usage, err = ReadCapacityPoolUsage(context.Background(), q, "signer", strings.Repeat("a", 64))
	if err != nil || usage.Nodes != 0 || usage.Monitoring.ReservedUtilization != 0 {
		t.Fatal(usage, err)
	}
}

func TestReadCapacityPoolUsageRejectsMalformedAggregate(t *testing.T) {
	at := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	q := &capacityUsageQuery{row: capacityUsageRow{used: 1, capacity: 0, nodes: 0, at: at}}
	if _, err := ReadCapacityPoolUsage(context.Background(), q, "cpu", strings.Repeat("a", 64)); err == nil {
		t.Fatal("reservation without capacity accepted")
	}
	q.calls = 0
	if _, err := ReadCapacityPoolUsage(context.Background(), q, "bad", strings.Repeat("a", 64)); err == nil || q.calls != 0 {
		t.Fatal("invalid pool queried")
	}
}
