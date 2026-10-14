package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestBodyFitDetectorEdgeRequiresOriginalLandmarkFaceBoxes(t *testing.T){
 for _,mode:=range []string{"valid","faces","image","hash","operation","contract"}{t.Run(mode,func(t *testing.T){
  _,_,image,mask:=bodyPersonFixture(t);ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(`{}`))}}
  landmark:=v2input.Entry{Contract:"FaceLandmarks",Ref:ref()};boxes:=v2input.Entry{Contract:"FaceBoxes",Ref:ref()}
  fitRaw,err:=canonical.Marshal(map[string]any{"params":map[string]any{"pair_id":uuid.New(),"before":image.Entry.Ref,"after":ref(),"alignment":ref(),"before_pose":ref(),"after_pose":ref(),"before_person":mask,"after_person":mask,"before_faces":landmark.Ref,"after_faces":ref()},"objects":[]any{}});if err!=nil{t.Fatal(err)}
  fit:=ExportClosureNode{Entry:v2input.Entry{Contract:"BodyFit",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(fitRaw)}},Body:fitRaw}
  params:=map[string]any{"image":image.Entry.Ref,"faces":boxes.Ref};if mode=="faces"{params["faces"]=ref()};if mode=="image"{params["image"]=ref()}
  raw,err:=canonical.Marshal(params);if err!=nil{t.Fatal(err)}
  origin:=ExportBodyDependencyOrigin{Producer:ExportParamsOperationEvidence{Operation:"face.landmarks",Version:"1"},Params:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)}}
  if mode=="hash"{origin.Params.SHA256=rawSHA([]byte("foreign"))};if mode=="operation"{origin.Producer.Operation="pose.detect"};if mode=="contract"{boxes.Contract="PoseSet"}
  edge,err:=exportBodyFitDetectorEdge(origin,fit,landmark,raw,boxes)
  if mode=="valid"{if err!=nil||edge.Image!=image.Entry.Ref{t.Fatal("detector side lost",err)}}else if err==nil||edge!=(exportBodyFitFeature{}){t.Fatal("foreign detector ancestry accepted")}
 })}
}
func TestDetectorOriginSQLRequiresDatabase(t *testing.T){
 source:=SQLExportDocumentCatalog{}
 if _,err:=source.LookupExportBodyFitDetectorOrigin(context.Background(),ExportBodyDependencyOrigin{},v2input.Entry{},nil,ExportClosureNode{},v2input.Entry{},nil,v2input.Entry{});err!=v2input.ErrInvalid{t.Fatal(err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=source.LookupExportBodyFitDetectorOrigin(ctx,ExportBodyDependencyOrigin{},v2input.Entry{},nil,ExportClosureNode{},v2input.Entry{},nil,v2input.Entry{});err!=context.Canceled{t.Fatal(err)}
}
