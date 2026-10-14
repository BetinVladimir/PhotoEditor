package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
type fitFaceBoxesStub struct{calls int;deny bool}
func(s *fitFaceBoxesStub)QualifyExportBodyFitFaceBoxes(_ context.Context,_ ExportBodyDependencyOrigin,_ v2input.Entry,raw []byte,fit ExportClosureNode,_ v2input.Entry,params []byte,image ExportClosureNode,_ v2input.Entry)error{
 s.calls++;raw[0]='!';fit.Body[0]='!';params[0]='!';image.Body[0]='!';if s.deny{return v2input.ErrInvalid};return nil
}
func TestFitFeatureOriginalRequiresActualSideAndFaceAncestry(t *testing.T){
 for _,mode:=range []string{"pose","faces","missing_face_authority","face_denied","foreign_image","producer_changed","changed_image","cancelled"}{t.Run(mode,func(t *testing.T){
  parent,raw,image,mask:=bodyPersonFixture(t)
  ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(`{}`))}}
  bp,bf:=ref(),ref();facesMode:=mode=="faces"||mode=="missing_face_authority"||mode=="face_denied"
  child:=v2input.Entry{Contract:"PoseSet",Ref:bp};operation:="pose.detect"
  if facesMode{child=v2input.Entry{Contract:"FaceLandmarks",Ref:bf};operation="face.landmarks"}
  fitRaw,err:=canonical.Marshal(map[string]any{"params":map[string]any{"pair_id":uuid.New(),"before":image.Entry.Ref,"after":ref(),"alignment":ref(),"before_pose":bp,"after_pose":ref(),"before_person":mask,"after_person":mask,"before_faces":bf,"after_faces":ref()},"objects":[]any{}});if err!=nil{t.Fatal(err)}
  fit:=ExportClosureNode{Entry:v2input.Entry{Contract:"BodyFit",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(fitRaw)}},Body:fitRaw}
  params:=map[string]any{"image":image.Entry.Ref};if facesMode{params["faces"]=ref()};if mode=="foreign_image"{params["image"]=ref()}
  original,err:=canonical.Marshal(params);if err!=nil{t.Fatal(err)}
  origin:=ExportBodyDependencyOrigin{Producer:ExportParamsOperationEvidence{Operation:operation,Version:"1"},Params:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(original)}}
  source:=&fitFeatureOriginStub{origin:origin,change:mode=="producer_changed"}
  nodes:=&exportNodeResolverStub{nodes:map[uuid.UUID]ExportClosureNode{image.Entry.Ref.ArtifactID:image}}
  faceAuthority:=&fitFaceBoxesStub{deny:mode=="face_denied"}
  policy:=BoundExportBodyFitFeatureOriginal{Source:source,Images:bodyOriginChangingDocuments{base:nodes,change:false},Faces:faceAuthority}
  if mode=="changed_image"{policy.Images=fitFeatureChangingImage{base:nodes}}
  if mode=="missing_face_authority"{policy.Faces=nil}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  err=policy.QualifyExportBodyFitFeatureOriginal(ctx,origin,parent,raw,fit,child,original)
  valid:=mode=="pose"||mode=="faces";if (err==nil)!=valid{t.Fatal("unexpected side qualification",err)}
  if valid&&(source.calls!=2||nodes.calls!=2){t.Fatal("final image/origin missing")}
  if mode=="faces"&&faceAuthority.calls!=1{t.Fatal("face detector chain bypassed")}
  if rawSHA(original)!=origin.Params.SHA256||rawSHA(image.Body)!=image.Entry.Ref.SHA256||rawSHA(fit.Body)!=fit.Entry.Ref.SHA256{t.Fatal("callback mutated evidence")}
 })}
}
type fitFeatureChangingImage struct{base *exportNodeResolverStub}
func(s fitFeatureChangingImage)ResolveExportNode(ctx context.Context,entry v2input.Entry)(ExportClosureNode,error){
 node,err:=s.base.ResolveExportNode(ctx,entry);if s.base.calls>1{node.Body=[]byte(`{"changed":true}`)};return node,err
}
