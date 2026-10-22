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


func TestRenderBatchRelightRecoveryAndFinalizationBothRun(t *testing.T) {
	for _, before := range []bool{false, true} {
		for _, finalized := range []bool{false, true} {
			for _, restoration := range []bool{false, true} {
				rows := []expansionRow{}
				for i := 0; i < 5; i++ {
					rows = append(rows, expansionRow{values: []any{false}})
				}
				rows = append(rows, expansionRow{values: []any{before}}, expansionRow{values: []any{finalized}})
				if restoration {
					rows = append(rows, expansionRow{values: []any{false}})
				}
				db := &renderBatchQueries{rows: rows}
				changed, err := (RenderBatchFinalizer{DB: db, Relight: true, Restoration: restoration}).ProcessOne(context.Background())
				wantCalls := 7
				if restoration { wantCalls++ }
				if err != nil || changed != (before || finalized) || len(db.calls) != wantCalls {
					t.Fatal(changed, err, db.calls)
				}
				if db.calls[5] != "SELECT photo.enqueue_next_v28_relight_before_media()" || db.calls[6] != "SELECT photo.finalize_v28_relight_selected_run()" {
					t.Fatal("relight recovery/finalization skipped", db.calls)
				}
				if restoration && db.calls[7] != "SELECT photo.finalize_v28_restoration_selected_run()" {
					t.Fatal("relight displaced restoration finalization", db.calls)
				}
			}
		}
	}
}

func TestRenderBatchRelightErrorPreservesCommittedProgress(t *testing.T) {
	failure := errors.New("relight database unavailable")
	for _, failRecovery := range []bool{false, true} {
		rows := []expansionRow{{values: []any{true}}}
		for i := 0; i < 4; i++ { rows = append(rows, expansionRow{values: []any{false}}) }
		if !failRecovery { rows = append(rows, expansionRow{values: []any{true}}) }
		rows = append(rows, expansionRow{err: failure})
		db := &renderBatchQueries{rows: rows}
		changed, err := (RenderBatchFinalizer{DB: db, Relight: true, Restoration: true}).ProcessOne(context.Background())
		wantCalls := 7
		if failRecovery { wantCalls = 6 }
		if !changed || !errors.Is(err, failure) || len(db.calls) != wantCalls {
			t.Fatal("relight failure lost committed progress or continued mutation", changed, err, db.calls)
		}
	}
}

func TestRenderBatchLocalPortraitRecoveryDoesNotDisplaceOtherFinalizers(t *testing.T) {
 for _, local := range []bool{false, true} {
  rows := []expansionRow{}
  for i:=0;i<5;i++ { rows=append(rows,expansionRow{values:[]any{false}}) }
  rows=append(rows,expansionRow{values:[]any{local}},expansionRow{values:[]any{false}},expansionRow{values:[]any{false}},expansionRow{values:[]any{false}})
  db:=&renderBatchQueries{rows:rows}
  changed,err:=(RenderBatchFinalizer{DB:db,LocalPortrait:true,Relight:true,Restoration:true}).ProcessOne(context.Background())
  if err!=nil || changed!=local || len(db.calls)!=9 { t.Fatal(changed,err,db.calls) }
  if db.calls[5]!="SELECT photo.enqueue_next_v28_local_portrait_before_media()" || db.calls[6]!="SELECT photo.enqueue_next_v28_relight_before_media()" || db.calls[8]!="SELECT photo.finalize_v28_restoration_selected_run()" { t.Fatal("local recovery displaced another module",db.calls) }
 }
}

func TestRenderBatchLocalPortraitRecoveryFailurePreservesCommittedProgress(t *testing.T) {
 failure:=errors.New("local media database unavailable")
 rows:=[]expansionRow{{values:[]any{true}}}
 for i:=0;i<4;i++ { rows=append(rows,expansionRow{values:[]any{false}}) }
 rows=append(rows,expansionRow{err:failure})
 db:=&renderBatchQueries{rows:rows}
 changed,err:=(RenderBatchFinalizer{DB:db,LocalPortrait:true,Relight:true}).ProcessOne(context.Background())
 if !changed || !errors.Is(err,failure) || len(db.calls)!=6 { t.Fatal(changed,err,db.calls) }
}
