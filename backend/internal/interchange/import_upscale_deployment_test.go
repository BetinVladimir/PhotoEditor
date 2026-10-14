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
func TestUpscaleDeploymentRequiresExactScaleAndIndependentPolicy(t *testing.T){
 for _,mode:=range []string{"valid","4x","both","scale","operation","license","empty","pin","mutable","symlink","cancelled"}{t.Run(mode,func(t *testing.T){
  hash:=func(s string)string{return rawSHA([]byte(s))}
  policy:=importUpscalePolicyWire{Report:v2input.Ref{ArtifactID:uuid.New(),SHA256:hash("report")},Locks:v2qualification.DeploymentLocks{Operation:"image.upscale",Version:"1",Backend:"mps",ModelLockSHA:hash("model"),RuntimeLockSHA:hash("runtime"),DeviceProfileSHA:hash("device"),DatasetSHA:hash("dataset"),IssuerID:uuid.New().String(),RequiredChecks:[]string{"quality"},TestFixtureSHAs:[]string{hash("fixture")},EvidenceSHAs:[]string{hash("evidence")},LicenseCleared:true}}
  wire:=importUpscaleDeploymentWire{Schema:"interchange-upscale-qualification/v1",Scales:map[int]importUpscalePolicyWire{2:policy}}
  switch mode{case "4x":wire.Scales=map[int]importUpscalePolicyWire{4:policy};case "both":wire.Scales[4]=policy;case "scale":wire.Scales=map[int]importUpscalePolicyWire{3:policy};case "operation":policy.Locks.Operation="image.denoise";wire.Scales[2]=policy;case "license":policy.Locks.LicenseCleared=false;wire.Scales[2]=policy;case "empty":wire.Scales=map[int]importUpscalePolicyWire{}}
  raw,err:=canonical.Marshal(wire);if err!=nil{t.Fatal(err)};pin:=rawSHA(raw);if mode=="pin"{pin=hash("foreign")}
  path:=filepath.Join(t.TempDir(),"upscale.json");if err:=os.WriteFile(path,raw,0600);err!=nil{t.Fatal(err)}
  if mode=="mutable"{if err:=os.Chmod(path,0666);err!=nil{t.Fatal(err)}}
  if mode=="symlink"{link:=path+".link";if err:=os.Symlink(path,link);err!=nil{t.Fatal(err)};path=link}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  installation,err:=LoadImportUpscaleQualifications(ctx,path,pin)
  valid:=mode=="valid"||mode=="4x"||mode=="both"
  if (err==nil)!=valid{t.Fatal("unexpected upscale deployment",mode,err)}
  if valid&&len(installation)!=len(wire.Scales){t.Fatal("configured scale omitted")}
  if !valid&&installation!=nil{t.Fatal("partial qualification returned")}
 })}
}
