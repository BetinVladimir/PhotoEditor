package interchange
import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
func TestMaskPolicyInstallationRequiresCurrentSourceAndExactSelectorModel(t *testing.T){
 for _,mode:=range []string{"manual","model","unknown","lock","manual_model","source","revoked"}{t.Run(mode,func(t *testing.T){
  _,entry,raw:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(raw);if err!=nil{t.Fatal(err)};canvas,err:=canonical.Hash(image.Canvas);if err!=nil{t.Fatal(err)}
  binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:entry.Ref,CanvasHash:canvas}
  lock:=rawSHA([]byte("model"));mask:=review.MaskRef{Artifact:review.ArtifactRef{ID:uuid.New(),SHA256:rawSHA([]byte("pixels"))},Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"manual",Confidence:1,Protected:true}
  if mode=="model"||mode=="lock"||mode=="revoked"{mask.Selector="person";mask.ModelLockID=&lock}
  if mode=="unknown"{mask.Selector="uninstalled"};if mode=="manual_model"{mask.ModelLockID=&lock}
  calls:=0;sentinel:=errors.New("consent revoked")
  callback:=preparedInstallationMaskQualifier(func(_ context.Context,m review.MaskRef,_ v2input.SessionVersionBinding)error{calls++;if *m.ModelLockID!=lock{t.Fatal("model evidence changed")};m.TransformChain[0]='!';if mode=="revoked"&&calls==2{return sentinel};return nil})
  configured:=ImportMaskPolicyInstallation{Manual:true,Models:map[string]ImportModelMaskPolicy{"person":{ModelLock:lock,Qualifier:callback}}}
  masters:=&recipeSourceMasterReader{body:raw};targets:=&maskSemanticTargets{}
  installed,locks,err:=configured.Build(context.Background(),masters,targets);if err!=nil||len(locks)!=1||locks[0]!=lock{t.Fatal("mask policy installation",err)}
  delete(configured.Models,"person")
  if mode=="source"{targets.failure=sentinel};if mode=="lock"{foreign:=rawSHA([]byte("foreign"));mask.ModelLockID=&foreign}
  err=installed.QualifyImportedMask(context.Background(),mask,binding)
  valid:=mode=="manual"||mode=="model"
  if (err==nil)!=valid{t.Fatal("unexpected mask admission",mode,err)}
  if mode=="manual"&&calls!=0{t.Fatal("manual mask borrowed model authority")}
  if mode=="model"&&(calls!=2||masters.calls!=3||targets.calls!=3){t.Fatal("model source/policy repeat missing")}
  if mode=="revoked"&&!errors.Is(err,sentinel){t.Fatal("current consent refusal lost",err)}
 })}
}
func TestMaskPolicyInstallationRefusesMissingAndInvalidModels(t *testing.T){
 for _,config:=range []ImportMaskPolicyInstallation{{},{Models:map[string]ImportModelMaskPolicy{"manual":{}}},{Models:map[string]ImportModelMaskPolicy{"person":{ModelLock:"bad"}}}}{
  if _,err:=config.owned();err==nil{t.Fatal("invalid policy configuration accepted")}
 }
}
