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
func TestStatsPersonOperatorConfigRequiresValidIndependentPolicy(t *testing.T){
 for _,mode:=range []string{"valid","operation","schema","report","overlap","license","mutable","symlink","cancelled"}{t.Run(mode,func(t *testing.T){
  digest:=func(value string)string{return rawSHA([]byte(value))}
  config:=ExportStatsPersonPolicyConfig{Schema:1,Report:v2input.Ref{ArtifactID:uuid.New(),SHA256:digest("report")},Locks:v2qualification.DeploymentLocks{Operation:"mask.person",Version:"1",Backend:"mps",ModelLockSHA:digest("model"),RuntimeLockSHA:digest("runtime"),DeviceProfileSHA:digest("device"),DatasetSHA:digest("dataset"),IssuerID:uuid.New().String(),RequiredChecks:[]string{"coverage"},CalibrationSHAs:[]string{digest("calibration")},TestFixtureSHAs:[]string{digest("test")},EvidenceSHAs:[]string{digest("evidence")},LicenseCleared:true}}
  switch mode{case "operation":config.Locks.Operation="face.parse";case "schema":config.Schema=2;case "report":config.Report.ArtifactID=uuid.Nil;case "overlap":config.Locks.TestFixtureSHAs=config.Locks.CalibrationSHAs;case "license":config.Locks.LicenseCleared=false}
  raw,err:=canonical.Marshal(config);if err!=nil{t.Fatal(err)}
  path:=filepath.Join(t.TempDir(),"person.json");if err:=os.WriteFile(path,raw,0600);err!=nil{t.Fatal(err)}
  if mode=="mutable"{if err:=os.Chmod(path,0666);err!=nil{t.Fatal(err)}}
  if mode=="symlink"{link:=path+".link";if err:=os.Symlink(path,link);err!=nil{t.Fatal(err)};path=link}
  ctx:=context.Background();if mode=="cancelled"{var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  installation,err:=LoadExportStatsPersonPolicyConfig(ctx,path)
  if mode=="valid"{if err!=nil||installation.Report!=config.Report||installation.Expected.Backend!="mps"{t.Fatal("valid deployment config refused",err)}}else if err==nil{t.Fatal("invalid deployment policy accepted",mode)}
 })}
}
