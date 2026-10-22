package workflow

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"testing"
	"time"
)

type previewCleanupRow struct {
	value bool
	err   error
}

func (r previewCleanupRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*bool) = r.value
	return nil
}

type previewCleanupDatabase struct {
	queries []string
	failure int
	changed int
	bounded bool
	cancelAt int
	cancel context.CancelFunc
}

func (d *previewCleanupDatabase) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	d.queries = append(d.queries, query)
	n := len(d.queries)
	deadline, ok := ctx.Deadline()
	d.bounded = ok && time.Until(deadline) <= 15*time.Second
	if n == d.cancelAt && d.cancel != nil { d.cancel() }
	if n == d.failure {
		return previewCleanupRow{err: errors.New("database unavailable")}
	}
	return previewCleanupRow{value: n == d.changed}
}
func TestDerivativeCleanupAlsoReleasesIndependentPreviewQuota(t *testing.T) {
	for _, failure := range []int{0, 4, 5, 6, 7} {
		db := &previewCleanupDatabase{failure: failure, changed: 7}
		changed, err := (RenderDerivativeCleanup{DB: db}).ProcessOne(context.Background())
		if failure != 0 {
			if err == nil || len(db.queries) != failure {
				t.Fatal("failed cleanup continued", failure)
			}
			continue
		}
		if err != nil || !changed || !db.bounded || len(db.queries) != 7 || db.queries[6] != "SELECT photo.release_next_semantic_preapply_preview_quota()" {
			t.Fatal("semantic cleanup missing", err)
		}
	}
}
func TestDerivativeCleanupCancellationAtFinalReceiptIsNotHealthy(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	db := &previewCleanupDatabase{changed: 7, cancelAt: 7, cancel: cancel}
	changed, err := (RenderDerivativeCleanup{DB: db}).ProcessOne(ctx)
	if !changed || !errors.Is(err, context.Canceled) || len(db.queries) != 7 {
		t.Fatal("final cancellation reported healthy", changed, err)
	}
}
func TestDerivativeCleanupCancellationAvoidsDatabase(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	db := &previewCleanupDatabase{}
	if _, err := (RenderDerivativeCleanup{DB: db}).ProcessOne(ctx); err != context.Canceled || len(db.queries) != 0 {
		t.Fatal("cancelled cleanup reached database")
	}
}

func TestDerivativeCleanupStopsAtEveryCancelledCommittedReceipt(t *testing.T) {
 for step:=1;step<=7;step++ {
  ctx,cancel:=context.WithCancel(context.Background())
  db:=&previewCleanupDatabase{changed:step,cancelAt:step,cancel:cancel}
  changed,err:=(RenderDerivativeCleanup{DB:db}).ProcessOne(ctx)
  cancel()
  if !changed||!errors.Is(err,context.Canceled)||len(db.queries)!=step {
   t.Fatal("committed cleanup progress lost or cancelled pass continued",step,changed,err,len(db.queries))
  }
 }
}
func TestDerivativeCleanupPreservesEarlierCommittedProgressOnLaterError(t *testing.T) {
 db:=&previewCleanupDatabase{changed:1,failure:2}
 changed,err:=(RenderDerivativeCleanup{DB:db}).ProcessOne(context.Background())
 if !changed||err==nil||len(db.queries)!=2{t.Fatal("earlier committed receipt lost",changed,err)}
}
