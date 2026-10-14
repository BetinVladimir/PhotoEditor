package delivery

import (
 "context"
 "errors"
 "testing"
)

func TestMetadataSourceSlotCancellationAndRelease(t *testing.T){
 slots:=make(chan struct{},1)
 release,err:=acquireMetadataSourceSlot(context.Background(),slots)
 if err!=nil||len(slots)!=1{t.Fatal("first slot",err)}
 ctx,cancel:=context.WithCancel(context.Background())
 result:=make(chan error,1)
 go func(){next,err:=acquireMetadataSourceSlot(ctx,slots);if next!=nil{next()};result<-err}()
 cancel()
 if err:=<-result;!errors.Is(err,context.Canceled)||len(slots)!=1{t.Fatal("cancelled waiter consumed slot",err)}
 release();release()
 if len(slots)!=0{t.Fatal("release did not return slot")}
 next,err:=acquireMetadataSourceSlot(context.Background(),slots)
 if err!=nil{t.Fatal(err)};next()
}
func TestMetadataSourceSlotHandoff(t *testing.T){
 slots:=make(chan struct{},1)
 release,err:=acquireMetadataSourceSlot(context.Background(),slots);if err!=nil{t.Fatal(err)}
 acquired:=make(chan func(),1)
 go func(){next,err:=acquireMetadataSourceSlot(context.Background(),slots);if err!=nil{acquired<-nil;return};acquired<-next}()
 select{case next:=<-acquired:if next!=nil{next()};t.Fatal("second process admitted before release");default:}
 release()
 next:=<-acquired;if next==nil{t.Fatal("handoff refused")};next()
 if len(slots)!=0{t.Fatal("handoff leaked permit")}
}
