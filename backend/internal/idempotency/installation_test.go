package idempotency

import (
 "context"
 "errors"
 "testing"
 "time"

 "github.com/jackc/pgx/v5"
)

type installationReader struct { ready bool; err error; bounded bool }
func(db *installationReader)QueryRow(ctx context.Context,_ string,_ ...any)pgx.Row{
 deadline,ok:=ctx.Deadline();db.bounded=ok && time.Until(deadline)<=5*time.Second
 return receiptRow{scan:func(dst ...any)error{*(dst[0].(*bool))=db.ready;return db.err}}
}
func TestParentBindingInstallationRequiresColumnAndPrivileges(t *testing.T){
 for _,ready:=range []bool{false,true}{
  db:=&installationReader{ready:ready}
  err:=RequireParentBinding(context.Background(),db)
  if (err==nil)!=ready || !db.bounded{t.Fatalf("ready=%v bounded=%v err=%v",ready,db.bounded,err)}
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=RequireParentBinding(ctx,nil);!errors.Is(err,context.Canceled){t.Fatal(err)}
}
