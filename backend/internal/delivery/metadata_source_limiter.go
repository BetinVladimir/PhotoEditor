package delivery

import (
 "context"
 "sync"
)

// One extraction process per API process. The same budget is shared by all
// tenants, preparer copies and reconstructed per-request dependencies.
var metadataSourceProcessSlots = make(chan struct{}, 1)

func acquireMetadataSourceSlot(ctx context.Context,slots chan struct{})(func(),error){
 if err:=ctx.Err();err!=nil{return nil,err}
 select{
 case slots<-struct{}{}:
  if err:=ctx.Err();err!=nil{<-slots;return nil,err}
  var once sync.Once
  return func(){once.Do(func(){<-slots})},nil
 case <-ctx.Done():return nil,ctx.Err()
 }
}
