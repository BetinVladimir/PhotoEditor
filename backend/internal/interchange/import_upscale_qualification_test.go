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
type upscaleOperationPolicyFunc func(context.Context,string,string,[]byte,v2input.SessionVersionBinding)error
func(f upscaleOperationPolicyFunc)QualifyImportedLayerOperation(ctx context.Context,op,version string,raw []byte,b v2input.SessionVersionBinding)error{return f(ctx,op,version,raw,b)}
func TestImportedUpscaleBindsInstalledModelAndCurrentCanonicalSource(t *testing.T){
 for _,mode:=range []string{"valid","lock","source","canvas","master_changed","revoked","targets","cancelled"}{t.Run(mode,func(t *testing.T){
  _,entry,master:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(master);if err!=nil{t.Fatal(err)};canvas,err:=canonical.Hash(image.Canvas);if err!=nil{t.Fatal(err)}
  binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:entry.Ref,CanvasHash:canvas}
  lock:=rawSHA([]byte("model"));p:=importUpscaleParams{Image:binding.Source,Scale:2,Model:"realesrgan_x2",Lock:lock,Adapter:"work_srgb_delta_v1",Tile:256,Overlap:32}
  switch mode{case "lock":p.Lock=rawSHA([]byte("foreign"));case "source":p.Image.ArtifactID=uuid.New();case "canvas":binding.CanvasHash=rawSHA([]byte("other"))}
  raw,err:=canonical.Marshal(p);if err!=nil{t.Fatal(err)}
  sentinel:=errors.New("qualification revoked");calls:=0
  policy:=upscaleOperationPolicyFunc(func(_ context.Context,op,version string,body []byte,b v2input.SessionVersionBinding)error{
   calls++;if string(body)!=string(raw)||op!="image.upscale"||version!="1"||b!=binding{t.Fatal("policy evidence changed")};body[0]='!';if mode=="revoked"&&calls==2{return sentinel};return nil
  })
  masters:=&recipeSourceMasterReader{body:master};if mode=="master_changed"{masters.second=[]byte(`{}`)}
  targets:=&maskSemanticTargets{};if mode=="targets"{targets.failure=sentinel}
  q:=BoundImportUpscaleOperation{Masters:masters,Targets:targets,ModelLocks:map[int]string{2:lock},Policy:policy}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  err=q.QualifyImportedLayerOperation(ctx,"image.upscale","1",raw,binding)
  if (err==nil)!=(mode=="valid"){t.Fatal("unexpected canonical upscale admission",err)}
  if mode=="valid"&&(calls!=2||masters.calls!=3||targets.calls!=3){t.Fatal("current policy/source repeat absent")}
  if mode=="lock"||mode=="source"||mode=="canvas"||mode=="targets"||mode=="cancelled"{if calls!=0{t.Fatal("invalid source reached runtime policy")}}
  if mode=="targets"||mode=="revoked"{if !errors.Is(err,sentinel){t.Fatal("policy refusal lost",err)}}
 })}
}
func TestImportUpscaleOutputSizeBoundAndOverflowRefusal(t *testing.T){
 for _,test:=range []struct{width,height int64;scale int;valid bool}{
  {10000,5000,2,true},{10000,5001,2,false},{5000,2500,4,true},{5000,2501,4,false},
  {1<<62,1<<62,4,false},{1,1<<62,2,false},{0,1,2,false},{1,1,3,false},
 }{
  width,height,err:=importUpscaleOutputSize(test.width,test.height,test.scale)
  if (err==nil)!=test.valid{t.Fatal("unexpected output size admission",test,err)}
  if test.valid&&(width!=test.width*int64(test.scale)||height!=test.height*int64(test.scale)){t.Fatal("output dimensions changed")}
  if !test.valid&&(width!=0||height!=0){t.Fatal("invalid size returned dimensions")}
 }
}
func TestImportUpscaleModelLockConfigurationOwned(t *testing.T){
 configured:=map[int]string{2:rawSHA([]byte("x2")),4:rawSHA([]byte("x4"))}
 owned,err:=ownedImportUpscaleLocks(configured);if err!=nil{t.Fatal(err)}
 configured[2]="changed";if owned[2]=="changed"{t.Fatal("model policy aliases caller")}
 for _,bad:=range []map[int]string{nil,{3:rawSHA([]byte("x3"))},{2:"invalid"}}{if _,err:=ownedImportUpscaleLocks(bad);err==nil{t.Fatal("invalid model policy accepted")}}
}
