package provenance

import (
 "context"
 "encoding/json"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

func TestDelegatedRenderRecipePinsFrozenBytes(t *testing.T) {
 binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),
  Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)},
  CanvasHash:strings.Repeat("b",64)}
 style:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("c",64)}
 authored,err:=canonical.Marshal(map[string]any{"schema_version":3,"source":binding,
  "base_style":style,"session_corrections":nil,"learning_version_id":nil,
  "overrides":[]v2input.Ref{},"layer_stack":nil,"renderer_version":"v2.2",
  "parameter_delta":v2draft.CorrectionVector{EV:.1}})
 if err!=nil{t.Fatal(err)}
 frozen,err:=v2draft.BuildFrozenRenderRecipe(authored,v2draft.ResolvedRecipeInputs{
  Binding:binding,BaseStyle:style,Style:v2draft.StyleBaseline{GainsRGB:[3]float64{1,1,1},
   Saturation:1},ICCLockSHA:strings.Repeat("d",64),ModelLockSHA:strings.Repeat("e",64)},nil)
 if err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"valid","hash","changed","cancelled"}{
  t.Run(mode,func(t *testing.T){
   task:=CanonicalSigningTask{Task:uuid.New(),Run:uuid.New(),Result:uuid.New(),
    Attempt:1,Fence:1,Operation:"render.compose",Version:"1",
    Params:v2input.Ref{ArtifactID:uuid.New(),SHA256:bodyDigest(frozen)},
    InputDigest:strings.Repeat("f",64),ResultDigest:strings.Repeat("a",64),
    Inputs:json.RawMessage(`[]`),Outputs:json.RawMessage(`[]`),
    Metrics:json.RawMessage(`{}`)}
   if mode=="hash"{task.Params.SHA256=strings.Repeat("0",64)}
   ctx,cancel:=context.WithCancel(context.Background());defer cancel()
   if mode=="cancelled"{cancel()}
   calls:=0
   docs:=delegatedDocumentsFunc(func(context.Context,CanonicalSigningTask,v2input.Ref,int)([]byte,error){
    calls++
    if mode=="changed"&&calls==2{return append(append([]byte(nil),frozen...),' '),nil}
    return frozen,nil
   })
   got,err:=ReadDelegatedRenderRecipe(ctx,docs,task)
   if mode=="valid"{
    if err!=nil||calls!=2||got.Scalars.Source!=binding{t.Fatal("valid recipe refused",err)}
   }else if err==nil{t.Fatal("invalid recipe accepted",mode)}
   if mode=="cancelled"&&calls!=0{t.Fatal("cancelled recipe performed IO")}
  })
 }
}
