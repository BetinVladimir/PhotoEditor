package workflow

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"testing"
)

type renderBatchQueries struct {
	rows  []expansionRow
	calls []string
}

func (q *renderBatchQueries) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	q.calls = append(q.calls, sql)
	row := q.rows[0]
	q.rows = q.rows[1:]
	return row
}
func TestRenderBatchPassDoesNotStarveIntermediateState(t *testing.T) {
	for _, completed := range []bool{false, true} {
		for _, synchronized := range []bool{false, true} {
			db := &renderBatchQueries{rows: []expansionRow{{values: []any{completed}}, {values: []any{synchronized}}}}
			changed, err := (RenderBatchFinalizer{DB: db}).ProcessOne(context.Background())
			if err != nil || changed != (completed || synchronized) || len(db.calls) != 2 {
				t.Fatal(completed, synchronized, changed, err, db.calls)
			}
			if db.calls[0] != "SELECT photo.finalize_v26_render_batch()" || db.calls[1] != "SELECT photo.sync_v26_render_batch_state()" {
				t.Fatal(db.calls)
			}
		}
	}
}
func TestRenderBatchPassPreservesErrorsAndCommittedProgress(t *testing.T) {
	failure := errors.New("database unavailable")
	for _, firstFailure := range []bool{false, true} {
		rows := []expansionRow{{values: []any{true}}, {err: failure}}
		wantCalls := 2
		if firstFailure {
			rows = []expansionRow{{err: failure}}
			wantCalls = 1
		}
		db := &renderBatchQueries{rows: rows}
		changed, err := (RenderBatchFinalizer{DB: db}).ProcessOne(context.Background())
		if !errors.Is(err, failure) || changed == firstFailure || len(db.calls) != wantCalls {
			t.Fatal(changed, err, db.calls)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	db := &renderBatchQueries{}
	if changed, err := (RenderBatchFinalizer{DB: db}).ProcessOne(ctx); changed || !errors.Is(err, context.Canceled) || len(db.calls) != 0 {
		t.Fatal(changed, err)
	}
}

func TestRenderBatchPassHandlesEmptyResultsWithoutSkippingSync(t *testing.T) {
	db := &renderBatchQueries{rows: []expansionRow{{err: pgx.ErrNoRows}, {values: []any{true}}}}
	changed, err := (RenderBatchFinalizer{DB: db}).ProcessOne(context.Background())
	if err != nil || !changed || len(db.calls) != 2 {
		t.Fatal(changed, err, db.calls)
	}
	db = &renderBatchQueries{rows: []expansionRow{{values: []any{true}}, {err: pgx.ErrNoRows}}}
	changed, err = (RenderBatchFinalizer{DB: db}).ProcessOne(context.Background())
	if err != nil || !changed || len(db.calls) != 2 {
		t.Fatal(changed, err, db.calls)
	}
}
