package interchange
import (
 "context"
 "testing"
)
func TestNativeDeploymentRequiresExplicitAuthorityFactory(t *testing.T){
 _,err:=(NativeExportDeployment{}).BuildWithAuthority(context.Background(),SQLExportExecutionInstaller{},nil)
 if err==nil{t.Fatal("missing authority factory accepted")}
}
