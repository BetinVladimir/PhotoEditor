package workflow

import (
 "context"
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/delivery"
)
func TestWatermarkProducerReaderRejectsForeignTaskAndScope(t *testing.T){
 lease:=delivery.ExpansionLease{Expansion:uuid.New(),Token:uuid.New(),Run:uuid.New()}
 request:=delivery.FrozenExportRequest{SchemaVersion:1,ParentRunID:lease.Run,ChildRunID:uuid.New(),SlotIndex:0}
 db:=&deliveryStageReadDB{responses:[][]byte{[]byte("null")}}
 result,err:=ReadWatermarkStageResult(context.Background(),db,lease,request,"photos");if err!=nil||result!=nil||db.calls!=1{t.Fatal("absent result",err)}
 foreign:=request;foreign.ParentRunID=uuid.New();before:=db.calls
 if _,err=ReadWatermarkStageResult(context.Background(),db,lease,foreign,"photos");err==nil||db.calls!=before{t.Fatal("foreign parent queried")}
 raw,_:=json.Marshal(DeliveryStageResult{TaskID:uuid.New(),ResultID:uuid.New(),Attempt:1,Fence:1})
 db=&deliveryStageReadDB{responses:[][]byte{raw}}
 if _,err=ReadWatermarkStageResult(context.Background(),db,lease,request,"photos");err==nil{t.Fatal("foreign task accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel();before=db.calls
 if _,err=ReadWatermarkStageResult(ctx,db,lease,request,"photos");err!=context.Canceled||db.calls!=before{t.Fatal("cancelled read",err)}
}
