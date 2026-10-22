package interchange

import (
 "context"
 "encoding/json"
 "errors"
 "os"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
type styleQualifierStub struct{validated,qualified int;failure error}
func(s *styleQualifierStub)ValidateArchivedImportStyle(_ context.Context,raw []byte)error{s.validated++;raw[0]='x';return s.failure}
func(s *styleQualifierStub)QualifyImportedStyle(_ context.Context,raw []byte,_ v2input.SessionVersionBinding)error{s.qualified++;raw[0]='x';return s.failure}
type backgroundStyleStub struct{calls int}
func(s *backgroundStyleStub)RemapImportedStyleBackground(_ context.Context,_ v2input.Ref,id string,_ v2input.SessionVersionBinding)(string,error){s.calls++;return "local-"+id,nil}
func TestStyleCodecPreservesRenderingParametersAndSeparatesHistoricalQuality(t *testing.T){
 for _,change:=range []string{"none","background","missing_background","qualification","cancelled"}{t.Run(change,func(t *testing.T){
  raw,err:=os.ReadFile("../run/identity_style.json");if err!=nil{t.Fatal(err)}
  var original map[string]json.RawMessage;if err:=json.Unmarshal(raw,&original);err!=nil{t.Fatal(err)}
  if change=="background"||change=="missing_background"{original["extensions"]=json.RawMessage(`{"background":{"mode":"asset","asset_id":"archive-bg"}}`)}
  raw,err=canonical.Marshal(original);if err!=nil{t.Fatal(err)}
  qualifier:=&styleQualifierStub{};sentinel:=errors.New("model not installed");if change=="qualification"{qualifier.failure=sentinel}
  assets:=&backgroundStyleStub{};codec:=ImportStyleCodec{Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Qualifier:qualifier};if change=="background"{codec.Backgrounds=assets}
  binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("master"))},CanvasHash:rawSHA([]byte("canvas"))}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if change=="cancelled"{cancel()}
  prepared,err:=codec.RemapImportDocument(ctx,"StyleV2",raw,binding,binding,nil)
  if change=="qualification"{if !errors.Is(err,sentinel)||assets.calls!=0{t.Fatal("unqualified model accepted",err)};return}
  if change=="cancelled"{if !errors.Is(err,context.Canceled){t.Fatal("cancellation lost",err)};return}
  if change=="missing_background"{var unsupported *ImportRecipeUnsupported;if !errors.As(err,&unsupported){t.Fatal("foreign background accepted",err)};return}
  if err!=nil{t.Fatal(err)}
  var result map[string]json.RawMessage;if err:=json.Unmarshal(prepared,&result);err!=nil{t.Fatal(err)}
  for _,key:=range []string{"schema_version","renderer_version","mode","strength","color","raw_recipe","models","global","local","adaptation","beauty","defects","geometry"}{left,leftErr:=canonical.Bytes(result[key]);right,rightErr:=canonical.Bytes(original[key]);if leftErr!=nil||rightErr!=nil||string(left)!=string(right){t.Fatal("processing parameter changed",key)}}
  if string(result["style_id"])==string(original["style_id"])||qualifier.validated!=1||qualifier.qualified!=1{t.Fatal("identity or qualification missing")}
  var quality map[string]json.RawMessage;if err:=json.Unmarshal(result["quality"],&quality);err!=nil||string(quality["status"])!=`"needs_review"`||string(quality["transfer_confidence"])!=`"unvalidated"`{t.Fatal("historical claims treated as current evidence",err)}
  if change=="background"{var extensions struct{Background struct{ID string `json:"asset_id"`} `json:"background"`};if err:=json.Unmarshal(result["extensions"],&extensions);err!=nil||extensions.Background.ID!="local-archive-bg"||assets.calls!=1{t.Fatal("background not remapped",err)}}
 })}
}
