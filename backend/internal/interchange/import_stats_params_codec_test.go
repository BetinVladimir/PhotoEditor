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
func TestImportStatsParamsRemapsSourceAndInlinePerson(t *testing.T){
 for _,mode:=range []string{"valid","source","canvas","selector","unchanged_pixels","mask_refused","operation_refused","cancelled"}{t.Run(mode,func(t *testing.T){
  _,_,imageRaw:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(imageRaw);if err!=nil{t.Fatal(err)}
  canvas,err:=canonical.Hash(image.Canvas);if err!=nil{t.Fatal(err)}
  old:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(imageRaw)},CanvasHash:canvas}
  target:=old;target.AssetVersionID=uuid.New();target.Source.ArtifactID=uuid.New()
  pixels:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("pixels"))}
  mask,err:=canonical.Marshal(map[string]any{"artifact":pixels,"canvas":image.Canvas,"transform_chain":json.RawMessage(image.TransformChain),"object_id":nil,"selector":"person","confidence":.8,"model_lock_id":rawSHA([]byte("model")),"protected":true});if err!=nil{t.Fatal(err)}
  if mode=="selector"{var fields map[string]any;if err:=json.Unmarshal(mask,&fields);err!=nil{t.Fatal(err)};fields["selector"]="manual";mask,err=canonical.Marshal(fields);if err!=nil{t.Fatal(err)}}
  params:=importStatsParams{Image:old.Source,Person:mask};if mode=="source"{params.Image.ArtifactID=uuid.New()};if mode=="canvas"{target.CanvasHash=rawSHA([]byte("other"))}
  raw,err:=canonical.Marshal(params);if err!=nil{t.Fatal(err)}
  sentinel:=errors.New("authority refused");maskPolicy:=&importMaskQualifierStub{};operation:=&preparedLayerOperationStub{}
  if mode=="mask_refused"{maskPolicy.failure=sentinel};if mode=="operation_refused"{operation.failure=sentinel}
  codec:=ImportStatsParamsCodec{Qualifier:operation,Masks:ImportMaskCodec{Qualifier:maskPolicy}}
  resolve:=func(ref v2input.Ref,contract string)(v2input.Ref,error){if contract!="MaskPixels"{t.Fatal("unexpected dependency",contract)};if mode=="unchanged_pixels"{return ref,nil};return v2input.Ref{ArtifactID:uuid.NewSHA1(target.AssetVersionID,[]byte(ref.ArtifactID.String())),SHA256:ref.SHA256},nil}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  result,err:=codec.RemapImportDocument(ctx,"Params",raw,old,target,resolve)
  if mode!="valid"{if err==nil||result!=nil{t.Fatal("invalid Stats accepted",err)};if (mode=="mask_refused"||mode=="operation_refused")&&!errors.Is(err,sentinel){t.Fatal("authority refusal lost",err)};return}
  if err!=nil{t.Fatal(err)};prepared,err:=decodeImportStatsParams(result);if err!=nil{t.Fatal(err)};person,err:=review.ParseMaskRef(prepared.Person);if err!=nil{t.Fatal(err)}
  if prepared.Image!=target.Source||person.Artifact.ID==pixels.ArtifactID||person.Artifact.SHA256!=pixels.SHA256||person.Confidence!=.8||!person.Protected||operation.calls!=1||operation.versions[0]!="color.stats:1"{t.Fatal("source/mask/control/operation lost")}
  entry:=v2input.Entry{Contract:"Params",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(result)}}
  children,err:=(ImportPreparedSchemaChildren{}).ImportDocumentChildren(ctx,entry,result);if err!=nil||len(children)!=2{t.Fatal("typed closure incomplete",children,err)}
  witnesses:=&ImportPreparedMaskWitnesses{};validator:=ImportPreparedMaskDocumentValidator{Binding:target,Qualifier:maskPolicy,Witnesses:witnesses,Other:ImportPreparedSchemaChildren{}}
  if _,err:=validator.ImportDocumentChildren(ctx,entry,result);err!=nil{t.Fatal(err)}
  child:=v2input.Entry{Contract:"MaskPixels",Ref:v2input.Ref{ArtifactID:person.Artifact.ID,SHA256:person.Artifact.SHA256}}
  witness,ok:=witnesses.lookup(child);if !ok||witness.contract!="Params"||witness.parent!=entry.Ref{t.Fatal("inline physical witness missing")}
  if err:=validateInlinePreparedMaskWitness(witness.body,witness.maskBody,child);err!=nil{t.Fatal(err)}
 })}
}
