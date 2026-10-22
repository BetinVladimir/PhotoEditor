package v2learning

import (
 "context"
 "errors"
 "testing"
 "time"
 "github.com/jackc/pgx/v5"
)

type trainingInstallationDB struct {
 ready bool
 err error
 calls int
 deadline time.Time
 cancel context.CancelFunc
}
type trainingInstallationRow struct { db *trainingInstallationDB }
func (d *trainingInstallationDB) QueryRow(ctx context.Context, _ string, _ ...any) pgx.Row {
 d.calls++
 d.deadline, _ = ctx.Deadline()
 return trainingInstallationRow{d}
}
func (r trainingInstallationRow) Scan(dest ...any) error {
 if r.db.cancel != nil { r.db.cancel() }
 if r.db.err != nil { return r.db.err }
 *dest[0].(*bool) = r.db.ready
 return nil
}

func TestTrainingReceiptInstallationRejectsMissingAuthorityAndBoundsStartup(t *testing.T) {
 if err := RequireAcceptedTrainingReceiptInstallation(context.Background(), nil); err == nil { t.Fatal("missing database accepted") }
 db := &trainingInstallationDB{}
 if err := RequireAcceptedTrainingReceiptInstallation(context.Background(), db); err == nil { t.Fatal("missing or wrong-role installation accepted") }
 if db.deadline.IsZero() || time.Until(db.deadline)>5*time.Second { t.Fatal("startup read is unbounded") }
 db.ready=true
 if err := RequireAcceptedTrainingReceiptInstallation(context.Background(), db); err != nil { t.Fatal(err) }
 failure:=errors.New("database unavailable")
 db.err=failure
 if err := RequireAcceptedTrainingReceiptInstallation(context.Background(), db); !errors.Is(err,failure) { t.Fatal(err) }
}

func TestTrainingReceiptInstallationCancellationCannotReportReady(t *testing.T) {
 ctx,cancel:=context.WithCancel(context.Background())
 cancel()
 db:=&trainingInstallationDB{ready:true}
 if err:=RequireAcceptedTrainingReceiptInstallation(ctx,db); !errors.Is(err,context.Canceled)||db.calls!=0 { t.Fatal("cancelled startup performed IO",err,db.calls) }
 ctx,cancel=context.WithCancel(context.Background())
 db.cancel=cancel
 if err:=RequireAcceptedTrainingReceiptInstallation(ctx,db); !errors.Is(err,context.Canceled) { t.Fatal("late ready escaped cancellation",err) }
}
