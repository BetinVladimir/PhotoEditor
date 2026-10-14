package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)
func TestBodyDependencyInstallationModelsAreClosedAndOwned(t *testing.T){
 for _,mode:=range []string{"valid","missing","duplicate","foreign","fit_backend","model_pin","report"}{t.Run(mode,func(t *testing.T){
  configs:=[]ExportBodyDependencyPolicyConfig{}
  for _,operation:=range []string{"pose.detect","face.landmarks","pair.fit.body"}{
   configs=append(configs,ExportBodyDependencyPolicyConfig{Report:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(operation))},Expected:v2qualification.Expected{Operation:operation,Version:"1",Backend:"cpu",ModelLockSHA:rawSHA([]byte(operation+"model")),LicenseCleared:true}})
  }
  switch mode{case "missing":configs=configs[:2];case "duplicate":configs[1].Expected.Operation=configs[0].Expected.Operation;case "foreign":configs[0].Expected.Operation="face.detect";case "fit_backend":configs[2].Expected.Backend="mps";case "model_pin":configs[0].Expected.ModelLockSHA="bad";case "report":configs[0].Report.ArtifactID=uuid.Nil}
  models,err:=exportBodyDependencyNativeModels(configs)
  if mode!="valid"{if err==nil||models!=nil{t.Fatal("invalid deployment model map accepted",err)};return}
  if err!=nil||len(models)!=2||models["keypointrcnn_resnet50_fpn"]!=configs[0].Expected.ModelLockSHA||models["fan2d"]!=configs[1].Expected.ModelLockSHA{t.Fatal("closed model mapping",models,err)}
  configs[0].Expected.ModelLockSHA="caller mutation";if models["keypointrcnn_resnet50_fpn"]=="caller mutation"{t.Fatal("model map aliases config")}
 })}
}
func TestBodyDependencyFactoryRejectsIncompleteScope(t *testing.T){
 installation:=SQLExportBodyDependencyInstallation{}
 if value,err:=installation.BuildExportBodyDependencyQualifier(context.Background(),CreateIntent{},FrozenExportDocuments{},SQLExportDocumentCatalog{},nil);err!=v2input.ErrInvalid||value!=nil{t.Fatal(err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if value,err:=installation.BuildExportBodyDependencyQualifier(ctx,CreateIntent{},FrozenExportDocuments{},nil,nil);err!=context.Canceled||value!=nil{t.Fatal(err)}
}
func TestExportAuthorityPreflightRequiresSignatures(t *testing.T){
 query:=&exportMaskInstallationQuery{row:exportMaskInstallationRow{installed:true}}
 if err:=validateExportAuthorityFunctions(context.Background(),query,nil);err!=v2input.ErrInvalid||query.calls!=0{t.Fatal("empty authority inventory accepted",err)}
 if err:=validateExportAuthorityFunctions(context.Background(),query,[]string{"photo.v30_body_dependency_qualification_object(uuid,uuid,uuid,uuid,jsonb)"});err!=nil||query.calls!=1{t.Fatal(err)}
}
