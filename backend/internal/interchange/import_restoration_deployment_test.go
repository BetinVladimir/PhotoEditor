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
func TestRestorationDeploymentLoadsBothPoliciesWithoutMutatingBase(t *testing.T){
 lock:=func(operation string)v2qualification.DeploymentLocks{return v2qualification.DeploymentLocks{Operation:operation,Version:"1",Backend:"mps",ModelLockSHA:rawSHA([]byte("model")),RuntimeLockSHA:rawSHA([]byte("runtime")),DeviceProfileSHA:rawSHA([]byte("device")),DatasetSHA:rawSHA([]byte("dataset")),IssuerID:uuid.New().String(),RequiredChecks:[]string{"quality"},TestFixtureSHAs:[]string{rawSHA([]byte("fixture"))},EvidenceSHAs:[]string{rawSHA([]byte("evidence"))},LicenseCleared:true}}
 report:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("report"))}
 denoise,err:=canonical.Marshal(importDenoiseDeploymentWire{Schema:"interchange-denoise-qualification/v1",Report:report,Locks:lock("image.denoise")});if err!=nil{t.Fatal(err)}
 upscale,err:=canonical.Marshal(importUpscaleDeploymentWire{Schema:"interchange-upscale-qualification/v1",Scales:map[int]importUpscalePolicyWire{2:{Report:report,Locks:lock("image.upscale")}}});if err!=nil{t.Fatal(err)}
 root:=t.TempDir();d,u:=filepath.Join(root,"denoise.json"),filepath.Join(root,"upscale.json")
 if err:=os.WriteFile(d,denoise,0600);err!=nil{t.Fatal(err)};if err:=os.WriteFile(u,upscale,0600);err!=nil{t.Fatal(err)}
 files:=ImportRestorationDeploymentFiles{DenoiseFile:d,DenoiseSHA:rawSHA(denoise),UpscaleFile:u,UpscaleSHA:rawSHA(upscale)}
 base:=SQLImportEntryRecipeAuthorityFactory{}
 installed,err:=files.Install(context.Background(),base);if err!=nil{t.Fatal(err)}
 if installed.Codecs.DenoiseQualification==nil||len(installed.Codecs.UpscaleQualifications)!=1||base.Codecs.DenoiseQualification!=nil||base.Codecs.UpscaleQualifications!=nil{t.Fatal("partial or aliased factory configuration")}
 if _,err:=files.Install(context.Background(),installed);err==nil{t.Fatal("existing policy silently overwritten")}
 files.UpscaleSHA=rawSHA([]byte("changed"))
 refused,err:=files.Install(context.Background(),base)
 if err==nil||refused.Codecs.DenoiseQualification!=nil||refused.Codecs.UpscaleQualifications!=nil{t.Fatal("failed second file yielded partial installation")}
 if base.Codecs.DenoiseQualification!=nil{t.Fatal("failed installation mutated base")}
}
func TestRestorationDeploymentRefusesIncompletePairsAndCancellation(t *testing.T){
 for _,files:=range []ImportRestorationDeploymentFiles{{DenoiseFile:"/operator/file"},{UpscaleSHA:rawSHA([]byte("file"))}}{if _,err:=files.Install(context.Background(),SQLImportEntryRecipeAuthorityFactory{});err==nil{t.Fatal("incomplete policy reference accepted")}}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=(ImportRestorationDeploymentFiles{}).Install(ctx,SQLImportEntryRecipeAuthorityFactory{});err!=context.Canceled{t.Fatal(err)}
}
