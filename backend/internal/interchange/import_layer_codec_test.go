package interchange

import (
 "context"
 "encoding/json"
 "errors"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)
type layerQualifierStub struct{calls int;failure error}
func(s *layerQualifierStub)QualifyImportedLayers(_ context.Context,raw []byte,_ v2input.SessionVersionBinding)error{s.calls++;raw[0]='x';return s.failure}
func TestLayerCodecPreservesLayerAndMaskExpressionOrder(t *testing.T){
 for _,change:=range []string{"none","canvas","unmapped","params_failure","qualification","cancelled"}{t.Run(change,func(t *testing.T){
  ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(uuid.NewString()))}}
  old:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:ref(),CanvasHash:rawSHA([]byte("canvas"))};target:=old;target.AssetVersionID=uuid.New();target.Source=ref()
  maskRef:=ref();leaf:=func()any{return map[string]any{"op":"ref","mask":maskRef,"canvas_hash":old.CanvasHash}}
  expression:=map[string]any{"op":"subtract","left":map[string]any{"op":"union","left":leaf(),"right":map[string]any{"op":"invert","operand":leaf()}},"right":map[string]any{"op":"intersect","left":leaf(),"right":leaf()}}
  layers:=[]map[string]any{
   {"id":uuid.New(),"enabled":true,"opacity":.37,"blend":"multiply","operation":"adjust.exposure","operation_version":"1","params":ref(),"mask":expression},
   {"id":uuid.New(),"enabled":false,"opacity":0,"blend":"screen","operation":"adjust.exposure","operation_version":"1","params":ref(),"mask":leaf()},
  }
  raw,err:=canonical.Marshal(map[string]any{"schema_version":1,"source":old,"layers":layers});if err!=nil{t.Fatal(err)}
  qualifier:=&layerQualifierStub{};sentinel:=errors.New("operator unavailable");if change=="qualification"{qualifier.failure=sentinel}
  if change=="canvas"{target.CanvasHash=rawSHA([]byte("other canvas"))}
  calls:=[]string{}
  resolve:=func(ref v2input.Ref,kind string)(v2input.Ref,error){calls=append(calls,kind);if change=="params_failure"&&kind=="Params"{return v2input.Ref{},sentinel};if change=="unmapped"{return ref,nil};return v2input.Ref{ArtifactID:uuid.NewSHA1(target.AssetVersionID,[]byte(ref.ArtifactID.String())),SHA256:ref.SHA256},nil}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if change=="cancelled"{cancel()}
  codec:=ImportLayerStackCodec{Qualifier:qualifier}
  prepared,err:=codec.RemapImportDocument(ctx,"LayerStack",raw,old,target,resolve)
  if change!="none"{if err==nil{t.Fatal("unqualified stack accepted")};if (change=="params_failure"||change=="qualification")&&!errors.Is(err,sentinel){t.Fatal("failure reclassified",err)};if change=="cancelled"&&!errors.Is(err,context.Canceled){t.Fatal("cancellation lost",err)};return}
  if err!=nil{t.Fatal(err)}
  if _,err:=v2draft.ValidateLayerStack(prepared,target);err!=nil{t.Fatal(err)}
  if qualifier.calls!=1||len(calls)!=7||calls[0]!="Params"||calls[5]!="Params"{t.Fatal("dependency order or disabled layer lost",calls)}
  var result struct{Layers []map[string]json.RawMessage `json:"layers"`};if err:=json.Unmarshal(prepared,&result);err!=nil{t.Fatal(err)}
  if len(result.Layers)!=2{t.Fatal("layer dropped")}
  for i,layer:=range result.Layers{
   for _,field:=range []string{"enabled","opacity","blend","operation","operation_version"}{expected,err:=canonical.Marshal(layers[i][field]);if err!=nil||string(expected)!=string(layer[field]){t.Fatal("authored parameter changed",field,err)}}
   var id uuid.UUID;if err:=json.Unmarshal(layer["id"],&id);err!=nil||id==layers[i]["id"].(uuid.UUID){t.Fatal("old layer UUID retained",err)}
  }
  var expr map[string]json.RawMessage;if err:=json.Unmarshal(result.Layers[0]["mask"],&expr);err!=nil||string(expr["op"])!=`"subtract"`{t.Fatal("mask operation changed",err)}
 })}
}
