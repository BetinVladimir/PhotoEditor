package interchange
import (
 "context"
 "encoding/json"
 "os"
 "path/filepath"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)
func TestPersonMaskOperatorConfigRequiresPinnedIndependentPolicy(t *testing.T){
 for _,mode:=range []string{"valid","operation","schema","report","overlap","license","mutable","symlink","cancelled","pin","consent_claim"}{t.Run(mode,func(t *testing.T){
  digest:=func(value string)string{return rawSHA([]byte(value))}
  config:=importPersonMaskDeploymentWire{Schema:"interchange-person-mask-qualification/v1",Report:v2input.Ref{ArtifactID:uuid.New(),SHA256:digest("report")},Locks:v2qualification.DeploymentLocks{Operation:"mask.person",Version:"1",Backend:"mps",ModelLockSHA:digest("model"),RuntimeLockSHA:digest("runtime"),DeviceProfileSHA:digest("device"),DatasetSHA:digest("dataset"),IssuerID:uuid.New().String(),RequiredChecks:[]string{"coverage"},CalibrationSHAs:[]string{digest("calibration")},TestFixtureSHAs:[]string{digest("test")},EvidenceSHAs:[]string{digest("evidence")},LicenseCleared:true}}
  switch mode{case "operation":config.Locks.Operation="face.parse";case "schema":config.Schema="unknown";case "report":config.Report.ArtifactID=uuid.Nil;case "overlap":config.Locks.TestFixtureSHAs=config.Locks.CalibrationSHAs;case "license":config.Locks.LicenseCleared=false}
  raw,err:=canonical.Marshal(config);if err!=nil{t.Fatal(err)}
  if mode=="consent_claim"{var fields map[string]any;if err:=json.Unmarshal(raw,&fields);err!=nil{t.Fatal(err)};fields["consent"]=true;raw,err=canonical.Marshal(fields);if err!=nil{t.Fatal(err)}}
  path:=filepath.Join(t.TempDir(),"prompt.json");if err:=os.WriteFile(path,raw,0600);err!=nil{t.Fatal(err)}
  if mode=="mutable"{if err:=os.Chmod(path,0666);err!=nil{t.Fatal(err)}}
  if mode=="symlink"{link:=path+".link";if err:=os.Symlink(path,link);err!=nil{t.Fatal(err)};path=link}
  ctx:=context.Background();if mode=="cancelled"{var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  pin:=rawSHA(raw);if mode=="pin"{pin=digest("foreign config")}
  installation,err:=LoadImportPersonMaskQualification(ctx,path,pin,&importMaskQualifierStub{})
  if mode=="valid"{if err!=nil||installation.Report!=config.Report||installation.Locks.Backend!="mps"{t.Fatal("valid deployment config refused",err)}}else if err==nil{t.Fatal("invalid deployment policy accepted",mode)}
 })}
}

func TestPersonMaskDeploymentRequiresSeparateConsentAndRejectsConflictingAuthority(t *testing.T){
 config:=importPersonMaskDeploymentWire{Schema:"interchange-person-mask-qualification/v1",Report:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("report"))},Locks:v2qualification.DeploymentLocks{Operation:"mask.person",Version:"1",Backend:"mps",ModelLockSHA:rawSHA([]byte("model")),RuntimeLockSHA:rawSHA([]byte("runtime")),DeviceProfileSHA:rawSHA([]byte("device")),DatasetSHA:rawSHA([]byte("dataset")),IssuerID:uuid.New().String(),RequiredChecks:[]string{"coverage"},TestFixtureSHAs:[]string{rawSHA([]byte("fixture"))},EvidenceSHAs:[]string{rawSHA([]byte("evidence"))},LicenseCleared:true}}
 raw,err:=canonical.Marshal(config);if err!=nil{t.Fatal(err)}
 path:=filepath.Join(t.TempDir(),"person.json");if err:=os.WriteFile(path,raw,0600);err!=nil{t.Fatal(err)}
 consent:=&importMaskQualifierStub{};files:=ImportPersonMaskDeploymentFile{Path:path,SHA:rawSHA(raw)}
 base:=SQLImportEntryRecipeAuthorityFactory{MaskInstallation:&ImportMaskPolicyInstallation{Manual:true}}
 installed,err:=files.Install(context.Background(),base,consent)
 if err!=nil||installed.PersonMaskQualification==nil||installed.PersonMaskQualification.Consent!=consent||base.PersonMaskQualification!=nil||consent.calls!=0{t.Fatal("installation issued authority or mutated input",err)}
 for _,existing:=range []SQLImportEntryRecipeAuthorityFactory{installed,{MaskSemantics:consent},{MaskInstallation:&ImportMaskPolicyInstallation{Models:map[string]ImportModelMaskPolicy{"person":{ModelLock:config.Locks.ModelLockSHA,Qualifier:consent}}}}}{
  if result,err:=files.Install(context.Background(),existing,consent);err==nil||result.PersonMaskQualification!=nil{t.Fatal("duplicate authority accepted",err)}
 }
 if _,err:=files.Install(context.Background(),base,nil);err==nil{t.Fatal("file manufactured consent")}
 if _,err:=LoadImportPersonMaskQualification(context.Background(),path,files.SHA,nil);err==nil{t.Fatal("loader manufactured consent")}
 if _,err:=DecodeImportPersonMaskQualification(context.Background(),raw,files.SHA,nil);err==nil{t.Fatal("decoder manufactured consent")}
}
