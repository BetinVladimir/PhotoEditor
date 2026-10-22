package queue

import (
 "context"
 "errors"
 "testing"
 "github.com/jackc/pgx/v5"
)
type outboxSchemaRow struct{ready bool;err error;after func()}
func(r outboxSchemaRow)Scan(dest ...any)error{if r.after!=nil{r.after()};if r.err!=nil{return r.err};*(dest[0].(*bool))=r.ready;return nil}
type outboxSchemaDB struct{row outboxSchemaRow;calls int}
func(d *outboxSchemaDB)QueryRow(context.Context,string,...any)pgx.Row{d.calls++;return d.row}
func TestOutboxMarkSchemaRefusesOldDatabase(t *testing.T){
 db:=&outboxSchemaDB{}
 if RequireOutboxMarkFence(context.Background(),db)==nil{t.Fatal("old schema accepted")}
 db.row.ready=true
 if err:=RequireOutboxMarkFence(context.Background(),db);err!=nil{t.Fatal(err)}
}
func TestOutboxMarkSchemaHonorsCancellation(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel();db:=&outboxSchemaDB{row:outboxSchemaRow{ready:true}}
 if !errors.Is(RequireOutboxMarkFence(ctx,db),context.Canceled)||db.calls!=0{t.Fatal("cancelled probe queried database")}
 ctx,cancel=context.WithCancel(context.Background());defer cancel();db.row.after=cancel
 if !errors.Is(RequireOutboxMarkFence(ctx,db),context.Canceled){t.Fatal("late successful probe ignored cancellation")}
}
