package portal

import (
 "context"
 "errors"
 "testing"
 "time"
 "github.com/jackc/pgx/v5"
)

type preparationInstallationDB struct { ready bool; err error; calls int; deadline time.Time; cancel context.CancelFunc }
type preparationInstallationRow struct { db *preparationInstallationDB }
func (d *preparationInstallationDB) QueryRow(ctx context.Context, _ string, _ ...any) pgx.Row {
 d.calls++; d.deadline,_=ctx.Deadline(); return preparationInstallationRow{d}
}
func (r preparationInstallationRow) Scan(dest ...any) error {
 if r.db.cancel!=nil { r.db.cancel() }
 if r.db.err!=nil { return r.db.err }
 *dest[0].(*bool)=r.db.ready; return nil
}
func TestPreparationInstallationRefusesMissingOrOverprivilegedBoundary(t *testing.T) {
 if err:=RequirePreparationRecoveryInstallation(context.Background(),nil);err==nil { t.Fatal("missing database accepted") }
 db:=&preparationInstallationDB{}
 if err:=RequirePreparationRecoveryInstallation(context.Background(),db);err==nil { t.Fatal("uninstalled or invalid ACL accepted") }
 if db.deadline.IsZero()||time.Until(db.deadline)>5*time.Second { t.Fatal("unbounded startup query") }
 db.ready=true
 if err:=RequirePreparationRecoveryInstallation(context.Background(),db);err!=nil { t.Fatal(err) }
 failure:=errors.New("database unavailable");db.err=failure
 if err:=RequirePreparationRecoveryInstallation(context.Background(),db);!errors.Is(err,failure) { t.Fatal(err) }
}
func TestPreparationInstallationCancellationRefusesLateReady(t *testing.T) {
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 db:=&preparationInstallationDB{ready:true}
 if err:=RequirePreparationRecoveryInstallation(ctx,db);!errors.Is(err,context.Canceled)||db.calls!=0 { t.Fatal(err,db.calls) }
 ctx,cancel=context.WithCancel(context.Background());db.cancel=cancel
 if err:=RequirePreparationRecoveryInstallation(ctx,db);!errors.Is(err,context.Canceled) { t.Fatal("late result accepted",err) }
}
