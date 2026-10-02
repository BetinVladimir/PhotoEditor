package workflow

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"testing"
)

type skipQueries struct {
	calls  int
	values []int
	errAt  int
}

func (q *skipQueries) QueryRow(context.Context, string, ...any) pgx.Row {
	q.calls++
	if q.calls == q.errAt {
		return expansionRow{err: errors.New("database unavailable")}
	}
	return expansionRow{values: []any{q.values[q.calls-1]}}
}
func TestIngestFailurePropagationBatch(t *testing.T) {
	q := &skipQueries{values: []int{1, 1, 0}}
	repo := IngestPlanRepository{DB: q}
	count, err := repo.SkipFailedInspection(context.Background(), 10)
	if err != nil || count != 2 || q.calls != 3 {
		t.Fatal(count, err)
	}
	q.calls = 0
	q.errAt = 2
	count, err = repo.SkipFailedInspection(context.Background(), 10)
	if err == nil || count != 1 {
		t.Fatal("partial count lost")
	}
	q.calls = 0
	q.errAt = 0
	q.values = []int{2}
	if _, err = repo.SkipFailedInspection(context.Background(), 1); err == nil {
		t.Fatal("impossible row count accepted")
	}
	q.calls = 0
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = repo.SkipFailedInspection(ctx, 10); err != context.Canceled || q.calls != 0 {
		t.Fatal("cancelled batch queried")
	}
}
