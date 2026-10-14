package workflow

import (
 "context"
 "errors"
 "reflect"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/delivery"
)

type watermarkPendingTx struct{watermarkParamsTx;raw []byte;failure error}
func(t *watermarkPendingTx)QueryRow(_ context.Context,q string,_ ...any)pgx.Row{
 if q!=`SELECT photo.v30_watermark_pending_child_slots($1,$2,$3)`{panic("unexpected pending query")}
 t.events=append(t.events,"query")
 if t.failure!=nil{return watermarkSourceQueryRow{failure:t.failure}}
 return deliveryActivationRow{value:t.raw}
}
type watermarkPendingPool struct{tx *watermarkPendingTx}
func(p watermarkPendingPool)BeginTx(context.Context,pgx.TxOptions)(pgx.Tx,error){return p.tx,nil}
func TestWatermarkPendingSlotsCommitValidDiscoveryOnly(t *testing.T){
 lease:=delivery.ExpansionLease{Tenant:uuid.New(),Run:uuid.New(),Expansion:uuid.New(),Token:uuid.New(),Cursor:4,SlotCount:4}
 for _,raw:=range []string{"[]","[0,2]","null","[2,1]","[0,0]","[4]"}{t.Run(raw,func(t *testing.T){
  tx:=&watermarkPendingTx{raw:[]byte(raw)}
  slots,err:=readWatermarkPendingSlots(context.Background(),watermarkPendingPool{tx},lease,3)
  valid:=raw=="[]"||raw=="[0,2]"
  want:=[]string{"tenant","query","rollback"};if valid{want=[]string{"tenant","query","commit","rollback"}}
  if (err==nil)!=valid||!reflect.DeepEqual(tx.events,want){t.Fatal(raw,slots,err,tx.events)}
  if !valid&&slots!=nil{t.Fatal("partial candidates leaked",slots)}
 })}
 failure:=errors.New("discovery unavailable");tx:=&watermarkPendingTx{failure:failure}
 if slots,err:=readWatermarkPendingSlots(context.Background(),watermarkPendingPool{tx},lease,3);slots!=nil||!errors.Is(err,failure){t.Fatal(slots,err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=readWatermarkPendingSlots(ctx,nil,lease,3);err!=context.Canceled{t.Fatal(err)}
}
