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
type denoiseOperationPolicyFunc func(context.Context,string,string,[]byte,v2input.SessionVersionBinding)error
func(f denoiseOperationPolicyFunc)QualifyImportedLayerOperation(ctx context.Context,op,version string,raw []byte,b v2input.SessionVersionBinding)error{return f(ctx,op,version,raw,b)}
func TestImportedDenoiseBindsInstalledModelAndCurrentCanonicalSource(t *testing.T){
 for _,mode:=range []string{"valid","lock","source","canvas","master_changed","revoked","targets","cancelled"}{t.Run(mode,func(t *testing.T){
  _,entry,master:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(master);if err!=nil{t.Fatal(err)};canvas,err:=canonical.Hash(image.Canvas);if err!=nil{t.Fatal(err)}
  binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:entry.Ref,CanvasHash:canvas}
  lock:=rawSHA([]byte("model"));p:=importDenoiseParams{Image:binding.Source,Strength:.5,Model:"scunet_color_real_psnr",Lock:lock,Adapter:"work_srgb_delta_v1",Tile:256,Overlap:64}
  switch mode{case "lock":p.Lock=rawSHA([]byte("foreign"));case "source":p.Image.ArtifactID=uuid.New();case "canvas":binding.CanvasHash=rawSHA([]byte("other"))}
  raw,err:=canonical.Marshal(p);if err!=nil{t.Fatal(err)}
  sentinel:=errors.New("qualification revoked");calls:=0
  policy:=denoiseOperationPolicyFunc(func(_ context.Context,op,version string,body []byte,b v2input.SessionVersionBinding)error{
   calls++;if string(body)!=string(raw)||op!="image.denoise"||version!="1"||b!=binding{t.Fatal("policy evidence changed")};body[0]='!';if mode=="revoked"&&calls==2{return sentinel};return nil
  })
  masters:=&recipeSourceMasterReader{body:master};if mode=="master_changed"{masters.second=[]byte(`{}`)}
  targets:=&maskSemanticTargets{};if mode=="targets"{targets.failure=sentinel}
  q:=BoundImportDenoiseOperation{Masters:masters,Targets:targets,ModelLock:lock,Policy:policy}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  err=q.QualifyImportedLayerOperation(ctx,"image.denoise","1",raw,binding)
  if (err==nil)!=(mode=="valid"){t.Fatal("unexpected canonical denoise admission",err)}
  if mode=="valid"&&(calls!=2||masters.calls!=3||targets.calls!=3){t.Fatal("current policy/source repeat absent")}
  if mode=="lock"||mode=="source"||mode=="canvas"||mode=="targets"||mode=="cancelled"{if calls!=0{t.Fatal("invalid source reached runtime policy")}}
  if mode=="targets"||mode=="revoked"{if !errors.Is(err,sentinel){t.Fatal("policy refusal lost",err)}}
 })}
}
