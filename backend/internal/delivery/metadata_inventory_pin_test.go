package delivery

import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
)
func TestMetadataInventoryPinPreflight(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=(CreateIntent{}).PinMetadataInventory(ctx,nil,uuid.Nil,FrozenMetadataInventory{},nil);!errors.Is(err,context.Canceled){t.Fatal(err)}
 if err:=(CreateIntent{}).PinMetadataInventory(context.Background(),nil,uuid.New(),FrozenMetadataInventory{},nil);err==nil{t.Fatal("unrestored inventory accepted")}
}
