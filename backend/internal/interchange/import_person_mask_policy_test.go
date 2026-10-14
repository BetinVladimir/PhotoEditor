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
func TestPersonMaskPolicyRequiresExactWholePersonModelAndCurrentConsent(t *testing.T){
 for _,mode:=range []string{"valid","selector","lock","object","consent_before","consent_after","runtime","cancel_runtime","missing_consent"}{t.Run(mode,func(t *testing.T){
  _,entry,raw:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(raw);if err!=nil{t.Fatal(err)};canvas,err:=canonical.Hash(image.Canvas);if err!=nil{t.Fatal(err)}
  binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:entry.Ref,CanvasHash:canvas};lock:=rawSHA([]byte("person-model"))
  mask:=review.MaskRef{Artifact:review.ArtifactRef{ID:uuid.New(),SHA256:rawSHA([]byte("pixels"))},Canvas:image.Canvas,TransformChain:append([]byte(nil),image.TransformChain...),Selector:"person",Confidence:.8,ModelLockID:&lock,Protected:true}
  switch mode{case "selector":mask.Selector="manual";case "lock":foreign:=rawSHA([]byte("foreign"));mask.ModelLockID=&foreign;case "object":id:=uuid.New();mask.ObjectID=&id}
  calls,runtimeCalls:=0,0;sentinel:=errors.New("authority revoked")
  var consent ImportMaskQualifier=preparedInstallationMaskQualifier(func(_ context.Context,m review.MaskRef,b v2input.SessionVersionBinding)error{calls++;if m.Selector!="person"||m.Confidence!=.8||b!=binding{t.Fatal("consent evidence mutated")};m.TransformChain[0]='!';if mode=="consent_before"||(mode=="consent_after"&&calls==2){return sentinel};return nil})
  if mode=="missing_consent"{consent=nil}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  runtime:=func()error{runtimeCalls++;if mode=="runtime"{return sentinel};if mode=="cancel_runtime"{cancel()};return nil}
  err=qualifyImportedPersonMaskPolicy(ctx,mask,binding,lock,consent,runtime)
  if (err==nil)!=(mode=="valid"){t.Fatal("unexpected person mask admission",err)}
  if mode=="valid"&&(calls!=2||runtimeCalls!=1){t.Fatal("current policy repeats missing")}
  if mode=="consent_before"&&runtimeCalls!=0{t.Fatal("denied mask reached report IO")}
  if mode=="selector"||mode=="lock"||mode=="object"||mode=="missing_consent"{if calls!=0||runtimeCalls!=0{t.Fatal("invalid mask reached authorities")}}
  if mode=="consent_before"||mode=="consent_after"||mode=="runtime"{if !errors.Is(err,sentinel){t.Fatal("refusal lost",err)}}
  if mode=="cancel_runtime"&&!errors.Is(err,context.Canceled){t.Fatal("cancellation lost",err)}
 })}
}
func TestPersonMaskInstallationRequiresIndependentConsentAndRuntime(t *testing.T){
 config:=ImportPersonMaskQualificationInstallation{}
 if policy,lock,err:=config.Build(context.Background(),SQLPreparedImportArtifactStore{},nil);err==nil||policy!=nil||lock!=""{t.Fatal("partial person installation accepted",err)}
}
