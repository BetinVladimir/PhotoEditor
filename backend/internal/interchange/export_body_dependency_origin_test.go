package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
func TestBodyDependencyProducerUsesTypedWorkerOperationRoles(t *testing.T){
 _,imageEntry,imageRaw:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(imageRaw);if err!=nil{t.Fatal(err)}
 ref:=func(name string)v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(name))}}
 poses,faces,fit:=ref("poses"),ref("faces"),ref("fit")
 raw,err:=canonical.Marshal(map[string]any{"image":imageEntry.Ref,"poses":poses,"faces":faces,"fit":fit,"person":review.MaskRef{Artifact:image.Pixels,Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"person",Confidence:1}});if err!=nil{t.Fatal(err)}
 parent:=v2input.Entry{Contract:"Params",Ref:ref("params")};parent.Ref.SHA256=rawSHA(raw)
 for _,tc:=range []struct{entry v2input.Entry;operation,role,resource string}{
  {v2input.Entry{Ref:poses,Contract:"PoseSet"},"pose.detect","pose_set","gpu"},
  {v2input.Entry{Ref:faces,Contract:"FaceLandmarks"},"face.landmarks","face_landmarks","gpu"},
  {v2input.Entry{Ref:fit,Contract:"BodyFit"},"pair.fit.body","body_fit","cpu"},
 }{
  operation,role,resource,err:=exportBodyDependencyProducer(parent,raw,tc.entry)
  if err!=nil||operation!=tc.operation||role!=tc.role||resource!=tc.resource{t.Fatal("worker producer mapping changed",operation,role,resource,err)}
  foreign:=tc.entry;foreign.Ref.ArtifactID=uuid.New();if _,_,_,err:=exportBodyDependencyProducer(parent,raw,foreign);err==nil{t.Fatal("foreign dependency accepted")}
  wrong:=tc.entry;wrong.Contract="FaceBoxes";if _,_,_,err:=exportBodyDependencyProducer(parent,raw,wrong);err==nil{t.Fatal("wrong contract accepted")}
 }
 if _,_,_,err:=exportBodyDependencyProducer(parent,append(raw,' '),v2input.Entry{Ref:poses,Contract:"PoseSet"});err==nil{t.Fatal("changed Params accepted")}
}
func TestBodyDependencyOriginRequiresDatabaseAndContext(t *testing.T){
 source:=SQLExportDocumentCatalog{}
 if out,err:=source.LookupExportBodyDependencyOrigin(context.Background(),v2input.Entry{},nil,v2input.Entry{});err!=v2input.ErrInvalid||out!=(ExportBodyDependencyOrigin{}){t.Fatal(err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if out,err:=source.LookupExportBodyDependencyOrigin(ctx,v2input.Entry{},nil,v2input.Entry{});err!=context.Canceled||out!=(ExportBodyDependencyOrigin{}){t.Fatal(err)}
}
