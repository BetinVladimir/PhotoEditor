package interchange
import (
 "context"
 "testing"
)
func TestNativeImportSchedulerFactoryRequiresCompleteInstallation(t *testing.T){
 continuation,err:=(NativeImportSchedulerFactory{}).BuildSchedulerInterchangeImport(context.Background(),nil,nil,"")
 if err==nil||continuation!=nil{t.Fatal("incomplete scheduler factory installed")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 _,err=(NativeImportSchedulerFactory{}).BuildSchedulerInterchangeImport(ctx,nil,nil,"")
 if err!=context.Canceled{t.Fatal("cancellation lost",err)}
}
