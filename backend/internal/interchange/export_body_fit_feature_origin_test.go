package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestBodyFitFeatureEdgeBindsExactSide(t *testing.T){
 for _,mode:=range []string{"before_pose","after_pose","before_faces","after_faces","foreign","contract","ambiguous","hash"}{t.Run(mode,func(t *testing.T){
  _,_,image,mask:=bodyPersonFixture(t)
  ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(`{}`))}}
  after,bp,ap,bf,af:=ref(),ref(),ref(),ref(),ref()
  if mode=="ambiguous"{ap=bp}
  params:=map[string]any{"pair_id":uuid.New(),"before":image.Entry.Ref,"after":after,"alignment":ref(),"before_pose":bp,"after_pose":ap,"before_person":mask,"after_person":mask,"before_faces":bf,"after_faces":af}
  raw,err:=canonical.Marshal(map[string]any{"params":params,"objects":[]any{}});if err!=nil{t.Fatal(err)}
  fit:=ExportClosureNode{Entry:v2input.Entry{Contract:"BodyFit",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)}},Body:raw}
  child:=v2input.Entry{Contract:"PoseSet",Ref:bp};expected:=exportBodyFitFeature{"pose.detect","pose_set",image.Entry.Ref}
  switch mode{case "after_pose":child.Ref=ap;expected.Image=after;case "before_faces":child=v2input.Entry{Contract:"FaceLandmarks",Ref:bf};expected.Operation="face.landmarks";expected.Role="face_landmarks";case "after_faces":child=v2input.Entry{Contract:"FaceLandmarks",Ref:af};expected=exportBodyFitFeature{"face.landmarks","face_landmarks",after};case "foreign":child.Ref=ref();case "contract":child.Contract="PairAlignment";case "hash":fit.Body=[]byte(`{}`)}
  feature,err:=exportBodyFitFeatureEdge(fit,child)
  valid:=mode=="before_pose"||mode=="after_pose"||mode=="before_faces"||mode=="after_faces"
  if valid{if err!=nil||feature!=expected{t.Fatal("side source lost",err)}}else if err==nil||feature!=(exportBodyFitFeature{}){t.Fatal("foreign or ambiguous feature accepted")}
 })}
}
func TestBodyFitFeatureSQLRequiresDatabase(t *testing.T){
 source:=SQLExportDocumentCatalog{}
 if _,err:=source.LookupExportBodyFitFeatureOrigin(context.Background(),v2input.Entry{},nil,ExportClosureNode{},v2input.Entry{});err!=v2input.ErrInvalid{t.Fatal(err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=source.LookupExportBodyFitFeatureOrigin(ctx,v2input.Entry{},nil,ExportClosureNode{},v2input.Entry{});err!=context.Canceled{t.Fatal(err)}
}
