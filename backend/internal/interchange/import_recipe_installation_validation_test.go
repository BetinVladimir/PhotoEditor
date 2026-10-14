package interchange
import (
 "context"
 "errors"
 "testing"
 "photoeditor/backend/internal/v2input"
)
// These ports are deliberately unusable: startup validation must not execute
// semantic policy, native code, publication or object reads.
type installationOnlyImportPort struct{}
func(installationOnlyImportPort)RemapImportDocument(context.Context,string,[]byte,v2input.SessionVersionBinding,v2input.SessionVersionBinding,func(v2input.Ref,string)(v2input.Ref,error))([]byte,error){panic("startup remap")}
func(installationOnlyImportPort)QualifyRemappedImportDocument(context.Context,string,[]byte,v2input.SessionVersionBinding)error{panic("startup qualify")}
func(installationOnlyImportPort)QualifyImportOverrideContent(context.Context,[]byte,v2input.SessionVersionBinding)error{panic("startup override")}
func TestImportRecipeStartupRequiresCompleteNonconflictingConfiguration(t *testing.T){
 for _,mode:=range []string{"valid","params","override","style","style_lock","style_models","mask_conflict","person_consent","person_duplicate","report","operation_duplicate","denoise_lock","upscale_lock","cancelled"}{t.Run(mode,func(t *testing.T){
  files:=recipeDeploymentFixture(t)
  base:=SQLImportEntryRecipeAuthorityFactory{MaskInstallation:&ImportMaskPolicyInstallation{Manual:true},StyleInstallation:&ImportNativeStyleInstallation{WorkICCSHA:rawSHA([]byte("icc")),StyleRuntimeSHA:rawSHA([]byte("style")),ImageRuntimeSHA:rawSHA([]byte("image")),Models:map[string]string{}},Codecs:ImportRecipeCodecDependencies{Params:installationOnlyImportPort{},OverrideContent:installationOnlyImportPort{}}}
  factory,err:=files.Install(context.Background(),base,&importMaskQualifierStub{});if err!=nil{t.Fatal(err)}
  switch mode{
  case "params":factory.Codecs.Params=nil
  case "override":factory.Codecs.OverrideContent=nil
  case "style":factory.StyleInstallation=nil
  case "style_lock":factory.StyleInstallation.StyleRuntimeSHA="bad"
  case "style_models":factory.StyleInstallation.Models=nil
  case "mask_conflict":factory.MaskSemantics=&importMaskQualifierStub{}
  case "person_consent":factory.PersonMaskQualification.Consent=nil
  case "person_duplicate":factory.MaskInstallation=&ImportMaskPolicyInstallation{Models:map[string]ImportModelMaskPolicy{"person":{ModelLock:rawSHA([]byte("model")),Qualifier:&importMaskQualifierStub{}}}}
  case "report":factory.Codecs.StatsQualification.Locks.Operation="image.denoise"
  case "operation_duplicate":factory.Codecs.Operations,err=NewImportOperationRouter(context.Background(),map[ImportOperationVersion]ImportLayerOperationQualifier{{"color.stats","1"}:&preparedLayerOperationStub{}});if err!=nil{t.Fatal(err)}
  case "denoise_lock":factory.Codecs.DenoiseModelLock=rawSHA([]byte("foreign"))
  case "upscale_lock":factory.Codecs.UpscaleModelLocks=map[int]string{4:rawSHA([]byte("foreign"))}
  }
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  err=(SQLImportSchedulerRecipeFactory{Authority:factory}).ValidateImportRecipeInstallation(ctx)
  if (err==nil)!=(mode=="valid"){t.Fatal("unexpected installation result",mode,err)}
  if mode=="cancelled"&&!errors.Is(err,context.Canceled){t.Fatal("cancellation lost",err)}
 })}
}
func TestImportRecipeStartupRejectsMissingAuthority(t *testing.T){
 if err:=(SQLImportSchedulerRecipeFactory{}).ValidateImportRecipeInstallation(context.Background());!errors.Is(err,v2input.ErrInvalid){t.Fatal("missing authority accepted",err)}
 if err:=(SQLImportEntryRecipeAuthorityFactory{}).ValidateImportRecipeInstallation(context.Background());!errors.Is(err,v2input.ErrInvalid){t.Fatal("missing configuration accepted",err)}
}
