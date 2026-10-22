package interactive

import (
 "context"
 "errors"
 "testing"
 "time"
 "github.com/jackc/pgx/v5"
)

type installationRow struct{ready bool;err error}
func(r installationRow)Scan(values ...any)error{if r.err!=nil{return r.err};*values[0].(*bool)=r.ready;return nil}
type installationDB struct{ready bool;err error;calls int;bounded bool;after context.CancelFunc}
func(d *installationDB)QueryRow(ctx context.Context,_ string,_ ...any)pgx.Row{
 d.calls++;deadline,ok:=ctx.Deadline();d.bounded=ok&&time.Until(deadline)>0&&time.Until(deadline)<=5*time.Second
 if d.after!=nil{d.after()};return installationRow{d.ready,d.err}
}
func TestInteractiveRuntimeInstallationFailsClosed(t *testing.T){
 for _,ready:=range []bool{false,true}{db:=&installationDB{ready:ready};err:=RequireRuntimeInstallation(context.Background(),db);if (err==nil)!=ready||db.calls!=1||!db.bounded{t.Fatal(ready,err,db)}}
 if RequireRuntimeInstallation(context.Background(),nil)==nil{t.Fatal("missing database accepted")}
 failure:=errors.New("database unavailable");if err:=RequireRuntimeInstallation(context.Background(),&installationDB{err:failure});!errors.Is(err,failure){t.Fatal(err)}
}
func TestInteractiveRuntimeInstallationRefusesLateReadiness(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel();db:=&installationDB{ready:true}
 if err:=RequireRuntimeInstallation(ctx,db);!errors.Is(err,context.Canceled)||db.calls!=0{t.Fatal(err,db.calls)}
 ctx,cancel=context.WithCancel(context.Background());defer cancel();db=&installationDB{ready:true,after:cancel}
 if err:=RequireRuntimeInstallation(ctx,db);!errors.Is(err,context.Canceled){t.Fatal(err)}
}
