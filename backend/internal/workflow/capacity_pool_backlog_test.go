package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

type capacityBacklogRow struct {
	count int64
	err   error
}

func (r capacityBacklogRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != 1 {
		return errors.New("invalid backlog scan")
	}
	*(dest[0].(*int64)) = r.count
	return nil
}

type capacityBacklogQuery struct {
	row   capacityBacklogRow
	calls int
}

func (q *capacityBacklogQuery) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	q.calls++
	return q.row
}

func TestReadCapacityPoolPendingBacklog(t *testing.T) {
	q := &capacityBacklogQuery{row: capacityBacklogRow{count: 1}}
	pending, err := ReadCapacityPoolPendingBacklog(context.Background(), q, "signer")
	if err != nil || !pending || q.calls != 1 {
		t.Fatal(pending, err, q.calls)
	}
	q.row.count = 0
	pending, err = ReadCapacityPoolPendingBacklog(context.Background(), q, "gpu")
	if err != nil || pending {
		t.Fatal(pending, err)
	}
	q.row.count = -1
	if _, err = ReadCapacityPoolPendingBacklog(context.Background(), q, "cpu"); err == nil {
		t.Fatal("negative backlog accepted")
	}
}

func TestReadCapacityPoolPendingBacklogRejectsInvalidSource(t *testing.T) {
	q := &capacityBacklogQuery{}
	if _, err := ReadCapacityPoolPendingBacklog(context.Background(), q, "other"); err == nil || q.calls != 0 {
		t.Fatal("invalid pool queried")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadCapacityPoolPendingBacklog(ctx, q, "gpu"); !errors.Is(err, context.Canceled) || q.calls != 0 {
		t.Fatal("cancelled source queried", err)
	}
}
