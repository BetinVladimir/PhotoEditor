package interchange
import (
 "context"
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
type bodyOriginChangingDocuments struct{base *exportNodeResolverStub;change bool}
func(s bodyOriginChangingDocuments)ResolveExportNode(ctx context.Context,entry v2input.Entry)(ExportClosureNode,error){
 node,err:=s.base.ResolveExportNode(ctx,entry);if s.change&&s.base.calls>2{node.Body=[]byte(`{"changed":true}`)};return node,err
}
// Fake dependency documents prove pin/epoch orchestration, not native schemas.
func TestBodyOriginReaderPinsDocumentsAndProducer(t *testing.T){
 for _,mode:=range []string{"valid","changed_document","fence","hash","missing","collision","cancelled"}{t.Run(mode,func(t *testing.T){
  _,source,imageRaw:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(imageRaw);if err!=nil{t.Fatal(err)}
  poseRaw:=[]byte(`{}`);poses:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(poseRaw)}
  if mode=="collision"{poses=source.Ref}
  params:=map[string]any{"image":source.Ref,"poses":poses,"person":review.MaskRef{Artifact:image.Pixels,Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"person",Confidence:1},"coefficients":map[string]any{"face_slim":0,"waist_slim":0,"hip_slim":0,"torso_slim":0,"leg_length":0}}
  raw,err:=canonical.Marshal(params);if err!=nil{t.Fatal(err)}
  entry:=v2input.Entry{Contract:"Params",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)}}
  if mode=="hash"{entry.Ref.SHA256=rawSHA([]byte("foreign"))}
  nodes:=&exportNodeResolverStub{nodes:map[uuid.UUID]ExportClosureNode{source.Ref.ArtifactID:{Entry:source,Body:imageRaw},poses.ArtifactID:{Entry:v2input.Entry{Ref:poses,Contract:"PoseSet"},Body:poseRaw}}}
  if mode=="missing"{delete(nodes.nodes,poses.ArtifactID)}
  authority:=&exportParamsOperationStub{evidence:ExportParamsOperationEvidence{Task:uuid.New(),Operation:"geometry.body",Version:"1",Attempt:1,Fence:1},change:mode=="fence"}
  reader:=ExportBodyOriginReader{Source:authority,Documents:bodyOriginChangingDocuments{base:nodes,change:mode=="changed_document"}}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  evidence,bundle,err:=reader.ReadExportBodyOrigin(ctx,entry,raw)
  if mode!="valid"{if err==nil||bundle!=nil||evidence!=(ExportParamsOperationEvidence{}){t.Fatal("invalid body origin returned",err)};return}
  var decoded exportBodyOriginBundle
  if err!=nil||json.Unmarshal(bundle,&decoded)!=nil||evidence!=authority.evidence||authority.calls!=2||nodes.calls!=4||decoded.Params.SHA!=entry.Ref.SHA256||decoded.Image.SHA!=source.Ref.SHA256||decoded.Poses.SHA!=poses.SHA256||decoded.Fit!=nil||decoded.Faces!=nil{t.Fatal("body bundle incomplete",err)}
 })}
}
