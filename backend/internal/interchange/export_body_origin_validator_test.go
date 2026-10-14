package interchange
import (
 "context"
 "os"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
type bodyOriginPolicyStub struct{calls int;denyAfter int;source *exportParamsOperationStub;change bool}
func(s *bodyOriginPolicyStub)QualifyExportBodyOrigin(_ context.Context,_ ExportParamsOperationEvidence,_ v2input.Entry,raw,bundle []byte)error{
 s.calls++;raw[0]='!';bundle[0]='!' // The validator must not share private buffers.
 if s.change{s.source.evidence.Fence++}
 if s.denyAfter>=0&&s.calls>s.denyAfter{return v2input.ErrInvalid};return nil
}
// Mock native receipts qualify orchestration only, never production content.
func TestBodyOriginNativeRequiresCurrentPolicyAndPostProbeEvidence(t *testing.T){
 for _,mode:=range []string{"valid","policy_before","policy_after","producer_changed","probe","cancelled"}{t.Run(mode,func(t *testing.T){
  _,imageEntry,imageRaw:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(imageRaw);if err!=nil{t.Fatal(err)}
  poses:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(`{}`))}
  raw,err:=canonical.Marshal(map[string]any{"image":imageEntry.Ref,"poses":poses,"person":review.MaskRef{Artifact:image.Pixels,Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"person",Confidence:1},"coefficients":map[string]any{"face_slim":0,"waist_slim":0,"hip_slim":0,"torso_slim":0,"leg_length":0}});if err!=nil{t.Fatal(err)}
  entry:=v2input.Entry{Contract:"Params",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)}}
  source:=&exportParamsOperationStub{evidence:ExportParamsOperationEvidence{Task:uuid.New(),Operation:"geometry.body",Version:"1",Attempt:1,Fence:1}}
  documents:=&exportNodeResolverStub{nodes:map[uuid.UUID]ExportClosureNode{imageEntry.Ref.ArtifactID:{Entry:imageEntry,Body:imageRaw},poses.ArtifactID:{Entry:v2input.Entry{Ref:poses,Contract:"PoseSet"},Body:[]byte(`{}`)}}}
  policy:=&bodyOriginPolicyStub{denyAfter:-1,source:source,change:mode=="producer_changed"};if mode=="policy_before"{policy.denyAfter=0};if mode=="policy_after"{policy.denyAfter=1}
  root:=t.TempDir();scratch:=&ExportArchiveScratch{root:root,maximum:1<<20};runtime:=rawSHA([]byte("runtime"));probe:=&exportStyleProbeStub{runtime:runtime};if mode=="probe"{probe.err=v2input.ErrInvalid}
  other:=&resolvedOriginOtherSpy{}
  validator,err:=NewNativeExportBodyOriginValidator(ExportBodyOriginReader{Source:source,Documents:documents},policy,other,scratch,probe,map[string]string{},runtime);if err!=nil{t.Fatal(err)}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  children,err:=validator.ExportDocumentChildren(ctx,entry,raw)
  if mode=="valid"{if err!=nil||len(children)!=1||policy.calls!=2||source.calls!=4||probe.calls!=1||other.calls!=1{t.Fatal("body origin path incomplete",err)}}else if err==nil||children!=nil{t.Fatal("unqualified body returned",err)}
  if (mode=="policy_before"||mode=="probe"||mode=="cancelled")&&other.calls!=0{t.Fatal("refusal bypassed")}
  files,err:=os.ReadDir(root);if err!=nil||len(files)!=0||scratch.used!=0{t.Fatal("body staging leaked",err)}
 })}
}
