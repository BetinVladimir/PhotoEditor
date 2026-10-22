package tenant

import (
 "context"
 "errors"
 "testing"
 "time"
 "github.com/jackc/pgx/v5"
)

type adminInstallationDB struct { ready bool; err error; calls int; deadline time.Time; cancel context.CancelFunc }
type adminInstallationRow struct { db *adminInstallationDB }
func (d *adminInstallationDB) QueryRow(ctx context.Context, _ string, _ ...any) pgx.Row {
 d.calls++; d.deadline,_=ctx.Deadline(); return adminInstallationRow{d}
}
func (r adminInstallationRow) Scan(dest ...any) error {
 if r.db.cancel!=nil { r.db.cancel() }
 if r.db.err!=nil { return r.db.err }
 *dest[0].(*bool)=r.db.ready; return nil
}
func TestAdminInstallationRefusesMissingOrOverprivilegedBoundary(t *testing.T) {
 if err:=RequireAdminInstallation(context.Background(),nil);err==nil { t.Fatal("missing database accepted") }
 db:=&adminInstallationDB{}
 if err:=RequireAdminInstallation(context.Background(),db);err==nil { t.Fatal("uninstalled or invalid ACL accepted") }
 if db.deadline.IsZero()||time.Until(db.deadline)>5*time.Second { t.Fatal("unbounded startup query") }
 db.ready=true
 if err:=RequireAdminInstallation(context.Background(),db);err!=nil { t.Fatal(err) }
 failure:=errors.New("database unavailable");db.err=failure
 if err:=RequireAdminInstallation(context.Background(),db);!errors.Is(err,failure) { t.Fatal(err) }
}
func TestAdminInstallationCancellationRefusesLateReady(t *testing.T) {
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 db:=&adminInstallationDB{ready:true}
 if err:=RequireAdminInstallation(ctx,db);!errors.Is(err,context.Canceled)||db.calls!=0 { t.Fatal(err,db.calls) }
 ctx,cancel=context.WithCancel(context.Background());db.cancel=cancel
 if err:=RequireAdminInstallation(ctx,db);!errors.Is(err,context.Canceled) { t.Fatal("late result accepted",err) }
}
