package interchange
import (
 "context"
 "errors"
 "testing"
 "photoeditor/backend/internal/v2input"
)
func TestNativeImportStyleInstallationRequiresCompleteDeployment(t *testing.T){
 i:=ImportNativeStyleInstallation{}
 style,backgrounds,err:=i.Build(context.Background(),nil,nil,nil,nil,PreparedImportArtifactPublisher{},ImportResolverDependencies{})
 if !errors.Is(err,v2input.ErrInvalid)||style!=nil||backgrounds!=nil{t.Fatalf("partial deployment: %v",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 style,backgrounds,err=i.Build(ctx,nil,nil,nil,nil,PreparedImportArtifactPublisher{},ImportResolverDependencies{})
 if !errors.Is(err,context.Canceled)||style!=nil||backgrounds!=nil{t.Fatalf("cancel: %v",err)}
}
func TestNativeImportStyleInstallationOwnsModelPolicy(t *testing.T){
 original:=ImportNativeStyleInstallation{Models:map[string]string{"model":"pin"}}
 first,second:=original.owned(),original.owned()
 original.Models["model"]="changed";first.Models["extra"]="foreign"
 if first.Models["model"]!="pin"||second.Models["model"]!="pin"||second.Models["extra"]!=""{t.Fatal("model policy aliases caller or another workflow")}
 if (ImportNativeStyleInstallation{}).owned().Models!=nil{t.Fatal("missing model policy became installed")}
}
