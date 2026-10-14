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
type preparedLayerReaderStub struct{bodies map[v2input.Entry][]byte;calls int;change bool}
func(s *preparedLayerReaderStub)ReadPreparedImportDocument(_ context.Context,e v2input.Entry,_ int64)([]byte,error){s.calls++;body:=append([]byte(nil),s.bodies[e]...);if s.change&&s.calls>2{return []byte(`{}`),nil};return body,nil}
type preparedLayerOperationStub struct{calls int;versions []string;failure error}
func(s *preparedLayerOperationStub)QualifyImportedLayerOperation(_ context.Context,operation,version string,body []byte,_ v2input.SessionVersionBinding)error{s.calls++;s.versions=append(s.versions,operation+":"+version);body[0]='x';return s.failure}
func TestPreparedLayersCheckDisabledOperationsMasksAndFinalAuthority(t *testing.T){
 for _,change:=range []string{"none","operation","mask","race","missing","cancelled"}{t.Run(change,func(t *testing.T){
  _,_,master:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(master);if err!=nil{t.Fatal(err)}
  canvas,_:=canonical.Hash(image.Canvas)
  binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(master)},CanvasHash:canvas}
  params:=[]byte(`{"schema_version":1}`);p:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(params)}
  mraw,err:=canonical.Marshal(map[string]any{"artifact":v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("pixels"))},"canvas":image.Canvas,"transform_chain":json.RawMessage(image.TransformChain),"selector":"manual","confidence":1,"protected":true});if err!=nil{t.Fatal(err)}
  m:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(mraw)}
  layers:=[]any{}
  for i:=0;i<2;i++{layers=append(layers,map[string]any{"id":uuid.New(),"enabled":i==0,"opacity":0,"blend":"normal","operation":"adjust.exposure","operation_version":"1","params":p,"mask":map[string]any{"op":"ref","mask":m,"canvas_hash":canvas}})}
  raw,_:=canonical.Marshal(map[string]any{"schema_version":1,"source":binding,"layers":layers})
  reader:=&preparedLayerReaderStub{bodies:map[v2input.Entry][]byte{{Ref:p,Contract:"Params"}:params,{Ref:m,Contract:"MaskRef"}:mraw},change:change=="race"}
  ops:=&preparedLayerOperationStub{};masks:=&importMaskQualifierStub{};failure:=errors.New("qualification failed")
  if change=="operation"{ops.failure=failure};if change=="mask"{masks.failure=failure}
  q:=ImportPreparedLayerQualifier{Documents:reader,Masks:masks,Operations:ops};if change=="missing"{q.Operations=nil}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if change=="cancelled"{cancel()}
  err=q.QualifyImportedLayers(ctx,raw,binding)
  if change=="none"{if err!=nil||ops.calls!=2||masks.calls!=2||reader.calls!=4{t.Fatal("disabled layer/shared dependency qualification",err,ops.calls,masks.calls,reader.calls)};return}
  if err==nil{t.Fatal("incomplete qualification accepted")}
  if (change=="operation"||change=="mask")&&!errors.Is(err,failure){t.Fatal("qualification failure lost",err)}
  if change=="cancelled"&&!errors.Is(err,context.Canceled){t.Fatal(err)}
 })}
}
