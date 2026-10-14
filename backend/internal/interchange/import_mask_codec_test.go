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
type importMaskQualifierStub struct{calls int;failure error}
func(s *importMaskQualifierStub)QualifyImportedMask(context.Context,review.MaskRef,v2input.SessionVersionBinding)error{s.calls++;return s.failure}
func TestMaskCodecRetainsAuthoredMaskMetadataAndQualifiesPhysicalDependency(t *testing.T){
 for _,change:=range []string{"none","canvas","unmapped","physical_failure","dependency_failure","cancelled"}{t.Run(change,func(t *testing.T){
  _,_,imageRaw:=importWorkingValidatorFixture(t)
  image,err:=review.ParseWorkingImage(imageRaw);if err!=nil{t.Fatal(err)}
  canvas,err:=canonical.Hash(image.Canvas);if err!=nil{t.Fatal(err)}
  object:=uuid.New();pixels:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("pixels"))}
  raw,err:=canonical.Marshal(map[string]any{"artifact":pixels,"canvas":image.Canvas,"transform_chain":json.RawMessage(image.TransformChain),"object_id":object,"selector":"person","confidence":.75,"model_lock_id":rawSHA([]byte("model")),"protected":true});if err!=nil{t.Fatal(err)}
  old:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(imageRaw)},CanvasHash:canvas}
  target:=old;target.AssetVersionID=uuid.New();target.Source.ArtifactID=uuid.New();if change=="canvas"{target.CanvasHash=rawSHA([]byte("foreign"))}
  qualifier:=&importMaskQualifierStub{};sentinel:=errors.New("physical qualification unavailable");if change=="physical_failure"{qualifier.failure=sentinel}
  codec:=ImportMaskCodec{Qualifier:qualifier}
  calls:=0
  resolve:=func(ref v2input.Ref,contract string)(v2input.Ref,error){calls++;if contract!="MaskPixels"&&contract!="DenseWarpField"{t.Fatal("untyped dependency")};if change=="dependency_failure"{return v2input.Ref{},sentinel};if change=="unmapped"{return ref,nil};return v2input.Ref{ArtifactID:uuid.NewSHA1(target.AssetVersionID,[]byte(ref.ArtifactID.String())),SHA256:ref.SHA256},nil}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if change=="cancelled"{cancel()}
  prepared,err:=codec.RemapImportDocument(ctx,"MaskRef",raw,old,target,resolve)
  if change!="none"{if err==nil{t.Fatal("unqualified mask accepted")};if (change=="physical_failure"||change=="dependency_failure")&&!errors.Is(err,sentinel){t.Fatal("physical failure reclassified",err)};if change=="cancelled"&&!errors.Is(err,context.Canceled){t.Fatal("cancelled",err)};return}
  result,err:=review.ParseMaskRef(prepared);if err!=nil{t.Fatal(err)}
  if calls<1||qualifier.calls!=1||result.Artifact.ID==pixels.ArtifactID||result.Artifact.SHA256!=pixels.SHA256||result.ObjectID==nil||*result.ObjectID==object||result.Selector!="person"||result.Confidence!=.75||!result.Protected||result.ModelLockID==nil||*result.ModelLockID!=rawSHA([]byte("model")){t.Fatal("mask metadata or new identity lost")}
  if result.Canvas!=image.Canvas{t.Fatal("canvas changed without qualification")}
 })}
}
