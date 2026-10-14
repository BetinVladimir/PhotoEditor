package workflow

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"testing"
)

type linesLifecycleRow struct {
	value bool
	err   error
}

func (r linesLifecycleRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*(dest[0].(*bool)) = r.value
	return nil
}

type linesLifecycleDB struct {
	rows    []linesLifecycleRow
	queries []string
	bounded bool
}

func (d *linesLifecycleDB) QueryRow(ctx context.Context, q string, _ ...any) pgx.Row {
	_, d.bounded = ctx.Deadline()
	d.queries = append(d.queries, q)
	row := d.rows[0]
	d.rows = d.rows[1:]
	return row
}
func TestPerspectiveLinesLifecycleOrderingAndCommittedProgress(t *testing.T) {
	queries := []string{"SELECT photo.expire_next_v29_perspective_lines_draft()", "SELECT photo.finalize_next_v29_perspective_lines()", "SELECT photo.finalize_failed_v29_perspective_lines()", "SELECT photo.release_next_v29_perspective_lines_quota()"}
	for mask := 0; mask < 16; mask++ {
		db := &linesLifecycleDB{}
		for i := 0; i < 4; i++ {
			db.rows = append(db.rows, linesLifecycleRow{value: mask&(1<<i) != 0})
		}
		progress, err := (PerspectiveLinesLifecycle{DB: db}).ProcessOne(context.Background())
		if err != nil || progress != (mask != 0) || !db.bounded || len(db.queries) != 4 {
			t.Fatal(mask, progress, err)
		}
		for i, q := range db.queries {
			if q != queries[i] {
				t.Fatal("wrong stage ordering", db.queries)
			}
		}
	}
	failure := errors.New("database unavailable")
	for stage := 0; stage < 4; stage++ {
		db := &linesLifecycleDB{}
		for i := 0; i < stage; i++ {
			db.rows = append(db.rows, linesLifecycleRow{value: true})
		}
		db.rows = append(db.rows, linesLifecycleRow{err: failure})
		progress, err := (PerspectiveLinesLifecycle{DB: db}).ProcessOne(context.Background())
		if !errors.Is(err, failure) || progress != (stage > 0) || len(db.queries) != stage+1 {
			t.Fatal("error did not stop later stages or lost committed progress", stage, progress, err)
		}
	}
}
func TestPerspectiveLinesLifecycleCancellationAndDependencies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (PerspectiveLinesLifecycle{}).ProcessOne(ctx); err != context.Canceled {
		t.Fatal(err)
	}
	if _, err := (PerspectiveLinesLifecycle{}).ProcessOne(context.Background()); err == nil {
		t.Fatal("missing database accepted")
	}
}
