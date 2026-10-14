package interchange
import (
 "context"
 "errors"
 "os"
 "path/filepath"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)
func recipeDeploymentFixture(t *testing.T)ImportRecipeDeploymentFiles{
 t.Helper()
 locks:=func(op string)v2qualification.DeploymentLocks{return v2qualification.DeploymentLocks{Operation:op,Version:"1",Backend:"mps",ModelLockSHA:rawSHA([]byte(op+":model")),RuntimeLockSHA:rawSHA([]byte("runtime")),DeviceProfileSHA:rawSHA([]byte("device")),DatasetSHA:rawSHA([]byte("dataset")),IssuerID:uuid.New().String(),RequiredChecks:[]string{"quality"},TestFixtureSHAs:[]string{rawSHA([]byte("fixture"))},EvidenceSHAs:[]string{rawSHA([]byte("evidence"))},LicenseCleared:true}}
 report:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("report"))}}
 root:=t.TempDir()
 write:=func(name string,value any)(string,string){t.Helper();raw,err:=canonical.Marshal(value);if err!=nil{t.Fatal(err)};path:=filepath.Join(root,name);if err:=os.WriteFile(path,raw,0600);err!=nil{t.Fatal(err)};return path,rawSHA(raw)}
 d,ds:=write("denoise.json",importDenoiseDeploymentWire{Schema:"interchange-denoise-qualification/v1",Report:report(),Locks:locks("image.denoise")})
 u,us:=write("upscale.json",importUpscaleDeploymentWire{Schema:"interchange-upscale-qualification/v1",Scales:map[int]importUpscalePolicyWire{2:{Report:report(),Locks:locks("image.upscale")}}})
 s,ss:=write("stats.json",importStatsDeploymentWire{Schema:"interchange-stats-qualification/v1",Report:report(),Locks:locks("color.stats")})
 p,ps:=write("person.json",importPersonMaskDeploymentWire{Schema:"interchange-person-mask-qualification/v1",Report:report(),Locks:locks("mask.person")})
 return ImportRecipeDeploymentFiles{Restoration:ImportRestorationDeploymentFiles{DenoiseFile:d,DenoiseSHA:ds,UpscaleFile:u,UpscaleSHA:us},Stats:ImportStatsDeploymentFile{Path:s,SHA:ss},Person:ImportPersonMaskDeploymentFile{Path:p,SHA:ps}}
}
func TestRecipeDeploymentInstallationIsAtomicAcrossAllFiles(t *testing.T){
 for _,mode:=range []string{"valid","denoise","upscale","stats","person","missing_acceptance","duplicate","cancelled"}{t.Run(mode,func(t *testing.T){
  files:=recipeDeploymentFixture(t);bad:=rawSHA([]byte("wrong policy"))
  switch mode{case "denoise":files.Restoration.DenoiseSHA=bad;case "upscale":files.Restoration.UpscaleSHA=bad;case "stats":files.Stats.SHA=bad;case "person":files.Person.SHA=bad}
  base:=SQLImportEntryRecipeAuthorityFactory{MaskInstallation:&ImportMaskPolicyInstallation{Manual:true}}
  if mode=="duplicate"{base.Codecs.StatsQualification=&ImportStatsQualificationInstallation{}}
  var acceptance ImportMaskQualifier=&importMaskQualifierStub{};if mode=="missing_acceptance"{acceptance=nil}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  installed,err:=files.Install(ctx,base,acceptance)
  if mode=="valid"{
   if err!=nil||installed.Codecs.DenoiseQualification==nil||len(installed.Codecs.UpscaleQualifications)!=1||installed.Codecs.StatsQualification==nil||installed.PersonMaskQualification==nil{t.Fatal("incomplete installation",err)}
   if installed.PersonMaskQualification.Consent!=acceptance||!installed.MaskInstallation.Manual{t.Fatal("independent acceptance or manual configuration lost")}
  }else{
   if err==nil||installed.Codecs.DenoiseQualification!=nil||installed.Codecs.UpscaleQualifications!=nil||installed.Codecs.StatsQualification!=nil||installed.PersonMaskQualification!=nil{t.Fatal("partial installation escaped",err)}
   if mode=="cancelled"&&!errors.Is(err,context.Canceled){t.Fatal("cancellation lost",err)}
  }
  if base.Codecs.DenoiseQualification!=nil||base.Codecs.UpscaleQualifications!=nil||base.PersonMaskQualification!=nil||len(base.MaskInstallation.Models)!=0{t.Fatal("input factory mutated")}
  if mode!="duplicate"&&base.Codecs.StatsQualification!=nil{t.Fatal("input stats policy mutated")}
 })}
}
func TestRecipeDeploymentRefusesIncompleteDeclarationsBeforeIO(t *testing.T){
 files:=ImportRecipeDeploymentFiles{Restoration:ImportRestorationDeploymentFiles{DenoiseFile:"/not-opened",DenoiseSHA:rawSHA([]byte("pin"))},Person:ImportPersonMaskDeploymentFile{Path:"/missing-pair"}}
 if _,err:=files.Install(context.Background(),SQLImportEntryRecipeAuthorityFactory{},nil);!errors.Is(err,v2input.ErrInvalid){t.Fatal("declaration performed file IO",err)}
}
