package workflow
import (
 "context"
 "testing"
 "photoeditor/backend/internal/objectstore"
)
func TestImportSchedulerRejectsInvalidFactoryBeforeStartupIO(t *testing.T){
 for _,factories:=range [][]SchedulerInterchangeImportFactory{{nil},{nil,nil}}{
  if err:=RunSchedulerWithNativeRecovery(context.Background(),"","",objectstore.Config{},"",nil,factories...);err==nil{t.Fatal("invalid import factory accepted")}
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=RunSchedulerWithNativeRecovery(ctx,"","",objectstore.Config{},"",nil);err!=context.Canceled{t.Fatal("cancellation lost",err)}
}
