package interchange
import (
 "context"
 "encoding/json"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
func TestImportedStatsRequiresCurrentSourceMaskAndAlgorithm(t *testing.T){
 for _,mode:=range []string{"valid","source","canvas","mask_canvas","master_changed","mask_revoked","algorithm_revoked","targets","cancelled"}{t.Run(mode,func(t *testing.T){
  _,entry,master:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(master);if err!=nil{t.Fatal(err)};canvas,err:=canonical.Hash(image.Canvas);if err!=nil{t.Fatal(err)}
  binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:entry.Ref,CanvasHash:canvas}
  maskCanvas:=image.Canvas;if mode=="mask_canvas"{maskCanvas.SourceSHA256=rawSHA([]byte("foreign"))}
  person,err:=canonical.Marshal(map[string]any{"artifact":v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("pixels"))},"canvas":maskCanvas,"transform_chain":json.RawMessage(image.TransformChain),"object_id":nil,"selector":"person","confidence":1,"model_lock_id":rawSHA([]byte("model")),"protected":true});if err!=nil{t.Fatal(err)}
  p:=importStatsParams{Image:binding.Source,Person:person};if mode=="source"{p.Image.ArtifactID=uuid.New()};if mode=="canvas"{binding.CanvasHash=rawSHA([]byte("other"))}
  raw,err:=canonical.Marshal(p);if err!=nil{t.Fatal(err)}
  sentinel:=errors.New("current qualification revoked");policyCalls,maskCalls:=0,0
  policy:=denoiseOperationPolicyFunc(func(_ context.Context,op,version string,body []byte,b v2input.SessionVersionBinding)error{
   policyCalls++;if op!="color.stats"||version!="1"||b!=binding||string(body)!=string(raw){t.Fatal("algorithm evidence changed")};body[0]='!';if mode=="algorithm_revoked"&&policyCalls==2{return sentinel};return nil
  })
  masks:=preparedInstallationMaskQualifier(func(_ context.Context,m review.MaskRef,b v2input.SessionVersionBinding)error{maskCalls++;if m.Selector!="person"||b!=binding{t.Fatal("mask evidence changed")};m.TransformChain[0]='!';if mode=="mask_revoked"&&maskCalls==2{return sentinel};return nil})
  masters:=&recipeSourceMasterReader{body:master};if mode=="master_changed"{masters.second=[]byte(`{}`)}
  targets:=&maskSemanticTargets{};if mode=="targets"{targets.failure=sentinel}
  q:=BoundImportStatsOperation{Masters:masters,Targets:targets,Masks:masks,Policy:policy}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  err=q.QualifyImportedLayerOperation(ctx,"color.stats","1",raw,binding)
  if (err==nil)!=(mode=="valid"){t.Fatal("unexpected Stats admission",err)}
  if mode=="valid"&&(policyCalls!=2||maskCalls!=2||masters.calls!=3||targets.calls!=3){t.Fatal("current source/mask/algorithm repeat missing")}
  if mode=="source"||mode=="canvas"||mode=="mask_canvas"||mode=="targets"||mode=="cancelled"{if policyCalls!=0||maskCalls!=0{t.Fatal("invalid source reached policy")}}
  if mode=="mask_revoked"||mode=="algorithm_revoked"||mode=="targets"{if !errors.Is(err,sentinel){t.Fatal("qualification refusal lost",err)}}
 })}
}
