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
			for _, selected := range []bool{false, true} {
				db := &renderBatchQueries{rows: []expansionRow{{values: []any{completed}}, {values: []any{synchronized}}, {values: []any{selected}}, {values: []any{false}}, {values: []any{false}}}}
				changed, err := (RenderBatchFinalizer{DB: db}).ProcessOne(context.Background())
				if err != nil || changed != (completed || synchronized || selected) || len(db.calls) != 5 {
					t.Fatal(completed, synchronized, selected, changed, err, db.calls)
				}
				if db.calls[0] != "SELECT photo.finalize_v26_render_batch()" || db.calls[1] != "SELECT photo.sync_v26_render_batch_state()" || db.calls[2] != "SELECT photo.finalize_v26_render_selected_run()" || db.calls[3] != "SELECT photo.finalize_v28_open_eyes_selected_run()" || db.calls[4] != "SELECT photo.enqueue_next_v28_open_eyes_before_media()" {
					t.Fatal(db.calls)
				}
			}
		}
	}
}
func TestRenderBatchPassFinalizesRestorationWhenInstalled(t *testing.T) {
	for _, restored := range []bool{false, true} {
		db := &renderBatchQueries{rows: []expansionRow{{values: []any{false}}, {values: []any{false}}, {values: []any{false}}, {values: []any{false}}, {values: []any{false}}, {values: []any{restored}}}}
		changed, err := (RenderBatchFinalizer{DB: db, Restoration: true}).ProcessOne(context.Background())
		if err != nil || changed != restored || len(db.calls) != 6 || db.calls[5] != "SELECT photo.finalize_v28_restoration_selected_run()" {
			t.Fatal(changed, err, db.calls)
		}
	}
}
func TestRenderBatchPassFinalizesOpenEyesWithoutRestorationFlag(t *testing.T) {
	db := &renderBatchQueries{rows: []expansionRow{{values: []any{false}}, {values: []any{false}}, {values: []any{false}}, {values: []any{true}}, {values: []any{false}}}}
	changed, err := (RenderBatchFinalizer{DB: db}).ProcessOne(context.Background())
	if err != nil || !changed || len(db.calls) != 5 || db.calls[3] != "SELECT photo.finalize_v28_open_eyes_selected_run()" {
		t.Fatal(changed, err, db.calls)
	}
}
func TestRenderBatchPassRecoversOpenEyesBeforeMedia(t *testing.T) {
	db := &renderBatchQueries{rows: []expansionRow{{values: []any{false}}, {values: []any{false}}, {values: []any{false}}, {values: []any{false}}, {values: []any{true}}}}
	changed, err := (RenderBatchFinalizer{DB: db}).ProcessOne(context.Background())
	if err != nil || !changed || len(db.calls) != 5 || db.calls[4] != "SELECT photo.enqueue_next_v28_open_eyes_before_media()" {
		t.Fatal(changed, err, db.calls)
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
	db := &renderBatchQueries{rows: []expansionRow{{values: []any{true}}, {values: []any{false}}, {err: failure}}}
	if changed, err := (RenderBatchFinalizer{DB: db}).ProcessOne(context.Background()); !changed || !errors.Is(err, failure) || len(db.calls) != 3 {
		t.Fatal("selected finalization failure lost committed progress", changed, err, db.calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	db = &renderBatchQueries{}
	if changed, err := (RenderBatchFinalizer{DB: db}).ProcessOne(ctx); changed || !errors.Is(err, context.Canceled) || len(db.calls) != 0 {
		t.Fatal(changed, err)
	}
}

func TestRenderBatchPassHandlesEmptyResultsWithoutSkippingSync(t *testing.T) {
	db := &renderBatchQueries{rows: []expansionRow{{err: pgx.ErrNoRows}, {values: []any{true}}, {err: pgx.ErrNoRows}, {err: pgx.ErrNoRows}, {err: pgx.ErrNoRows}}}
	changed, err := (RenderBatchFinalizer{DB: db}).ProcessOne(context.Background())
	if err != nil || !changed || len(db.calls) != 5 {
		t.Fatal(changed, err, db.calls)
	}
	db = &renderBatchQueries{rows: []expansionRow{{values: []any{true}}, {err: pgx.ErrNoRows}, {err: pgx.ErrNoRows}, {err: pgx.ErrNoRows}, {err: pgx.ErrNoRows}}}
	changed, err = (RenderBatchFinalizer{DB: db}).ProcessOne(context.Background())
	if err != nil || !changed || len(db.calls) != 5 {
		t.Fatal(changed, err, db.calls)
	}
}
