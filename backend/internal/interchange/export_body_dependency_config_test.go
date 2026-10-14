package interchange
import (
 "context"
 "os"
 "path/filepath"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)
func TestBodyDependencyOperatorConfigRequiresCompleteIndependentInventory(t *testing.T){
 for _,mode:=range []string{"valid","missing","duplicate_operation","foreign_operation","shared_report","gpu_fit","overlap","license","schema","mutable","symlink","cancelled"}{t.Run(mode,func(t *testing.T){
  file:=ExportBodyDependencyPolicyFile{Schema:1}
  for _,operation:=range []string{"pose.detect","face.landmarks","pair.fit.body"}{
   backend:="mps";if operation=="pair.fit.body"{backend="cpu"}
   file.Policies=append(file.Policies,ExportBodyDependencyPolicyFileEntry{Report:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(operation))},Locks:v2qualification.DeploymentLocks{Operation:operation,Version:"1",Backend:backend,ModelLockSHA:rawSHA([]byte("model")),RuntimeLockSHA:rawSHA([]byte("runtime")),DeviceProfileSHA:rawSHA([]byte("device")),DatasetSHA:rawSHA([]byte("dataset")),IssuerID:uuid.New().String(),RequiredChecks:[]string{"quality"},CalibrationSHAs:[]string{rawSHA([]byte("calibration"))},TestFixtureSHAs:[]string{rawSHA([]byte("test"))},EvidenceSHAs:[]string{rawSHA([]byte("evidence"))},LicenseCleared:true}})
  }
  switch mode{case "missing":file.Policies=file.Policies[:2];case "duplicate_operation":file.Policies[1].Locks.Operation="pose.detect";case "foreign_operation":file.Policies[0].Locks.Operation="face.detect";case "shared_report":file.Policies[1].Report=file.Policies[0].Report;case "gpu_fit":file.Policies[2].Locks.Backend="mps";case "overlap":file.Policies[0].Locks.TestFixtureSHAs=file.Policies[0].Locks.CalibrationSHAs;case "license":file.Policies[0].Locks.LicenseCleared=false;case "schema":file.Schema=2}
  raw,err:=canonical.Marshal(file);if err!=nil{t.Fatal(err)}
  path:=filepath.Join(t.TempDir(),"body-policies.json");if err:=os.WriteFile(path,raw,0600);err!=nil{t.Fatal(err)}
  if mode=="mutable"{if err:=os.Chmod(path,0666);err!=nil{t.Fatal(err)}}
  if mode=="symlink"{link:=path+".link";if err:=os.Symlink(path,link);err!=nil{t.Fatal(err)};path=link}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  configs,err:=LoadExportBodyDependencyPolicyConfig(ctx,path)
  if mode=="valid"{if err!=nil||len(configs)!=3||configs[0].Report!=file.Policies[0].Report||configs[2].Expected.Backend!="cpu"{t.Fatal("valid config refused",err)}}else if err==nil||configs!=nil{t.Fatal("invalid operator inventory accepted",mode,err)}
 })}
}
