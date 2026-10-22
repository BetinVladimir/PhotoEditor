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
 "photoeditor/backend/internal/v2portrait"
)
type importLocalRuntimeStub struct{calls int;failAt int;failure error}
func(s *importLocalRuntimeStub)QualifyImportLocalRuntime(context.Context,string,string)error{s.calls++;if s.calls==s.failAt{return s.failure};return nil}
func TestImportedLocalOperationPhysicalDependenciesAndRuntimeRecheck(t *testing.T){
 for _,change:=range []string{"none","clothing","version","operation","runtime","runtime_revoked","mask","capacity","race","cancelled"}{t.Run(change,func(t *testing.T){
  _,_,master:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(master);if err!=nil{t.Fatal(err)};canvas,_:=canonical.Hash(image.Canvas)
  binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(master)},CanvasHash:canvas}
  mraw,err:=canonical.Marshal(map[string]any{"artifact":v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("pixels"))},"canvas":image.Canvas,"transform_chain":json.RawMessage(image.TransformChain),"object_id":nil,"model_lock_id":nil,"selector":"manual","confidence":1,"protected":true});if err!=nil{t.Fatal(err)}
  mask:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(mraw)}
  operation:="portrait.hair";version:="1"
  raw,_,err:=(v2portrait.HairParams{SchemaVersion:1,Base:binding,Mask:mask,Protected:mask,SaturationFactor:1}).Compile();if err!=nil{t.Fatal(err)}
  if change=="clothing"{operation="portrait.clothing";raw,_,err=(v2portrait.ClothingParams{SchemaVersion:1,Base:binding,Mask:mask,Protected:mask,SaturationFactor:1}).Compile();if err!=nil{t.Fatal(err)}}
  if change=="version"{version="2"};if change=="operation"{operation="portrait.clothing"}
  failure:=errors.New("authority revoked");runtime:=&importLocalRuntimeStub{failure:failure};if change=="runtime"{runtime.failAt=1};if change=="runtime_revoked"{runtime.failAt=2}
  masks:=&importMaskQualifierStub{};if change=="mask"{masks.failure=failure}
  documents:=&preparedLayerReaderStub{bodies:map[v2input.Entry][]byte{{Ref:mask,Contract:"MaskRef"}:mraw},change:change=="race"}
  masters:=&importMasterReaderStub{body:master};capacity,_:=v2portrait.LocalPortraitResources(image.Canvas.Width,image.Canvas.Height);if change=="capacity"{capacity.RAMBytes=0}
  q:=ImportLocalPortraitOperationQualifier{Masters:masters,Documents:documents,Masks:masks,Runtime:runtime,RuntimeSHA:rawSHA([]byte("runtime")),Capacity:capacity}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if change=="cancelled"{cancel()}
  err=q.QualifyImportedLayerOperation(ctx,operation,version,raw,binding)
  if change=="none"||change=="clothing"{if err!=nil||runtime.calls!=2||masks.calls!=2||masters.calls!=2||documents.calls!=4{t.Fatal("qualification/recheck",err)};return}
  if err==nil{t.Fatal("unqualified operation accepted")}
  if (change=="runtime"||change=="runtime_revoked"||change=="mask")&&!errors.Is(err,failure){t.Fatal("authority failure lost",err)}
  if change=="cancelled"&&!errors.Is(err,context.Canceled){t.Fatal(err)}
  if (change=="runtime"||change=="version"||change=="operation"||change=="cancelled")&&documents.calls!=0{t.Fatal("invalid request performed dependency IO")}
 })}
}
func TestSQLImportedLocalRuntimeRequiresInstallation(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=(SQLImportLocalRuntimeQualification{}).QualifyImportLocalRuntime(ctx,"portrait.hair","");!errors.Is(err,context.Canceled){t.Fatal(err)}
 if err:=(SQLImportLocalRuntimeQualification{}).QualifyImportLocalRuntime(context.Background(),"portrait.hair","");!errors.Is(err,v2input.ErrInvalid){t.Fatal(err)}
}
