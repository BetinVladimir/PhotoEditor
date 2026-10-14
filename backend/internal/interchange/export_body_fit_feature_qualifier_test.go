package interchange
import (
 "context"
 "os"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
type fitFeatureProfileStub struct{calls,denyAt int;change bool;source *fitFeatureOriginStub}
func(s *fitFeatureProfileStub)QualifyExportBodyFitFeatureProfile(_ context.Context,_ ExportBodyDependencyOrigin,_ v2input.Entry,raw []byte,_ ExportClosureNode,_ v2input.Entry,_ []byte)error{
 s.calls++;raw[0]='!';if s.change{s.source.origin.Producer.Fence++};if s.calls==s.denyAt{return v2input.ErrInvalid};return nil
}
// Fake documents/native receipts test mandatory phase orchestration only.
func TestNativeFitFeatureQualifierRechecksProfileAndOriginal(t *testing.T){
 for _,mode:=range []string{"valid","profile_before","profile_after","producer_changed","probe","output_size","cancelled"}{t.Run(mode,func(t *testing.T){
  _,imageEntry,imageRaw:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(imageRaw);if err!=nil{t.Fatal(err)}
  result:=[]byte(`{}`);poses:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(result)}
  raw,err:=canonical.Marshal(map[string]any{"image":imageEntry.Ref,"poses":poses,"person":review.MaskRef{Artifact:image.Pixels,Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"person",Confidence:1},"coefficients":map[string]any{"face_slim":0,"waist_slim":0,"hip_slim":0,"torso_slim":0,"leg_length":0}});if err!=nil{t.Fatal(err)}
  parent:=v2input.Entry{Contract:"Params",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)}}
  child:=ExportClosureNode{Entry:v2input.Entry{Contract:"PoseSet",Ref:poses},Body:result}
  params:=[]byte(`{}`);pin:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(params)}
  source:=&fitFeatureOriginStub{origin:ExportBodyDependencyOrigin{Producer:ExportParamsOperationEvidence{Task:uuid.New(),Operation:"pose.detect",Version:"1",Attempt:1,Fence:1},Params:pin,ParamsObject:objectstore.Object{Key:"params",VersionID:"immutable",SHA256:pin.SHA256,Bytes:2,ContentType:"application/json"},Output:objectstore.Object{Key:"poses",VersionID:"immutable",SHA256:poses.SHA256,Bytes:2,ContentType:"application/json"}}}
  if mode=="output_size"{source.origin.Output.Bytes=3}
  profile:=&fitFeatureProfileStub{source:source,change:mode=="producer_changed"};if mode=="profile_before"{profile.denyAt=1};if mode=="profile_after"{profile.denyAt=2}
  fitRaw,err:=canonical.Marshal(map[string]any{"params":map[string]any{"pair_id":uuid.New(),"before":imageEntry.Ref,"after":v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("after"))},"alignment":v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("alignment"))},"before_pose":poses,"after_pose":v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("after_pose"))},"before_person":review.MaskRef{Artifact:image.Pixels,Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"person",Confidence:1},"after_person":review.MaskRef{Artifact:image.Pixels,Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"person",Confidence:1}},"objects":[]any{}});if err!=nil{t.Fatal(err)}
  fit:=ExportClosureNode{Entry:v2input.Entry{Contract:"BodyFit",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(fitRaw)}},Body:fitRaw}
  root:=t.TempDir();scratch:=&ExportArchiveScratch{root:root,maximum:1<<20};runtime:=rawSHA([]byte("runtime"));probe:=&exportStyleProbeStub{runtime:runtime};if mode=="probe"{probe.err=v2input.ErrInvalid}
  qualifier,err:=NewNativeExportBodyFitFeatureQualifier(ExportBodyFitFeatureParamsReader{Source:source,Objects:statsParamsObjects{params}},profile,scratch,probe,map[string]string{},runtime);if err!=nil{t.Fatal(err)}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  err=qualifier.QualifyExportBodyFitFeature(ctx,parent,raw,fit,child)
  if mode=="valid"{if err!=nil||profile.calls!=2||source.calls!=4||probe.calls!=1{t.Fatal("body dependency chain incomplete",err)}}else if err==nil{t.Fatal("unqualified body dependency accepted")}
  if (mode=="profile_before"||mode=="output_size"||mode=="cancelled")&&probe.calls!=0{t.Fatal("preflight refusal reached probe")}
  if rawSHA(raw)!=parent.Ref.SHA256{t.Fatal("policy mutated caller")}
  files,err:=os.ReadDir(root);if err!=nil||len(files)!=0||scratch.used!=0{t.Fatal("dependency staging leaked",err)}
 })}
}

type fitFeatureOriginStub struct{origin ExportBodyDependencyOrigin;calls int;change bool}
func(s *fitFeatureOriginStub)LookupExportBodyFitFeatureOrigin(_ context.Context,_ v2input.Entry,raw []byte,fit ExportClosureNode,_ v2input.Entry)(ExportBodyDependencyOrigin,error){
 s.calls++;raw[0]='!';fit.Body[0]='!';out:=s.origin;if s.change&&s.calls>1{out.Producer.Fence++};return out,nil
}
