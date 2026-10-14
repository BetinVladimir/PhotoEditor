package interchange
import (
 "context"
 "testing"
)
func TestBodyDeploymentRequiresOperatorConfiguration(t *testing.T){
 if _,err:=LoadExportBodyInstallation(context.Background(),ExportBodyDeploymentFiles{},ExportBodyDeploymentRuntime{});err==nil{t.Fatal("empty deployment accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=LoadExportBodyInstallation(ctx,ExportBodyDeploymentFiles{},ExportBodyDeploymentRuntime{});err!=context.Canceled{t.Fatal("cancellation lost",err)}
}
