package maintenance

import (
 "context"
 "errors"
 "testing"
 "time"
 "github.com/jackc/pgx/v5"
)

type outputRefundDB struct { row gcAccountingRow; query string; deadline time.Time; calls int }
func (d *outputRefundDB) QueryRow(ctx context.Context, query string, _ ...any) pgx.Row {
 d.calls++;d.query=query;d.deadline,_=ctx.Deadline();return d.row
}
func TestOutputRefundStartupRefusesOldSchemaAndProbeErrors(t *testing.T){
 db:=&outputRefundDB{}
 if RequireOutputCleanupRefund(context.Background(),db)==nil{t.Fatal("old schema accepted")}
 if db.deadline.IsZero()||time.Until(db.deadline)>5*time.Second{t.Fatal("unbounded startup query")}
 expected:=errors.New("unavailable");db.row.err=expected
 if !errors.Is(RequireOutputCleanupRefund(context.Background(),db),expected){t.Fatal("query failure hidden")}
 db.row=gcAccountingRow{ready:true}
 if err:=RequireOutputCleanupRefund(context.Background(),db);err!=nil{t.Fatal(err)}
}
func TestOutputRefundStartupCancellationBeforeAndAfterProbe(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 db:=&outputRefundDB{row:gcAccountingRow{ready:true}}
 if !errors.Is(RequireOutputCleanupRefund(ctx,db),context.Canceled)||db.calls!=0{t.Fatal("cancelled startup queried")}
 ctx,cancel=context.WithCancel(context.Background());defer cancel()
 db.row.after=cancel
 if !errors.Is(RequireOutputCleanupRefund(ctx,db),context.Canceled){t.Fatal("late successful query escaped cancellation")}
}
