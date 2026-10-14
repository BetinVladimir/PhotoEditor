package workflow

import (
 "context"
 "errors"
 "testing"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)
func TestWatermarkEncodeTransportRejectsEmptyAndCancelled(t *testing.T){
 var frozen FrozenDeliveryWatermarkEncodeTask
 db:=&deliveryStageRecordDB{accepted:true}
 if _,err:=frozen.TransportRegistered(context.Background(),db,delivery.ExpansionLease{},delivery.FrozenExportRequest{});err==nil||db.calls!=0{t.Fatal("empty descriptor queried")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=frozen.TransportRegistered(ctx,db,delivery.ExpansionLease{},delivery.FrozenExportRequest{});!errors.Is(err,context.Canceled)||db.calls!=0{t.Fatal("cancel ignored",err)}
 if err:=frozen.RegisterInputsInTransaction(ctx,nil,delivery.ExpansionLease{},delivery.FrozenExportRequest{},v2input.GraphTask{});!errors.Is(err,context.Canceled){t.Fatal("register cancel ignored",err)}
 if err:=frozen.RegisterInputsInTransaction(context.Background(),nil,delivery.ExpansionLease{},delivery.FrozenExportRequest{},v2input.GraphTask{});err==nil{t.Fatal("nil transaction accepted")}
}

// Scripted receipt/activation outcomes distinguish replay from first dispatch.
type watermarkActivationDB struct{answers []bool;queries []string}
func(d *watermarkActivationDB)QueryRow(_ context.Context,q string,_ ...any)pgx.Row{
 d.queries=append(d.queries,q);n:=len(d.queries)-1
 if n>=len(d.answers){panic("unexpected activation query")}
 return deliveryActivationRow{value:d.answers[n]}
}
