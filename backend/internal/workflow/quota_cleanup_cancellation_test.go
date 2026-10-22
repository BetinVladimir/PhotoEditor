package workflow

import (
 "context"
 "errors"
 "testing"
 "github.com/jackc/pgx/v5"
)

// Cancellation may race a committed SQL receipt. Preserve its progress bit,
// but never advertise the expired cleanup pass as healthy to the loop.
type cancellingQuotaRow struct { cancel context.CancelFunc }
func (r cancellingQuotaRow) Scan(dest ...any) error {
 *(dest[0].(*bool)) = true
 r.cancel()
 return nil
}
type cancellingQuotaDB struct { cancel context.CancelFunc; calls int }
func (d *cancellingQuotaDB) QueryRow(context.Context,string,...any) pgx.Row {
 d.calls++
 return cancellingQuotaRow{cancel:d.cancel}
}

func TestQuotaCleanupReceiptCancellationPreservesProgressAndStopsPass(t *testing.T) {
 for _,kind:=range []string{"negative","replacement","lines"} {
  t.Run(kind,func(t *testing.T){
   ctx,cancel:=context.WithCancel(context.Background());defer cancel()
   db:=&cancellingQuotaDB{cancel:cancel}
   var job ExpansionJob
   switch kind {
   case "negative":job=NegativeCancellationQuota{DB:db}
   case "replacement":job=ReplacementDetectorQuotaCleanup{DB:db}
   case "lines":job=PerspectiveLinesLifecycle{DB:db}
   }
   changed,err:=job.ProcessOne(ctx)
   if !changed||!errors.Is(err,context.Canceled)||db.calls!=1 {
    t.Fatal("cancelled receipt lost progress or continued pass",changed,err,db.calls)
   }
  })
 }
}
