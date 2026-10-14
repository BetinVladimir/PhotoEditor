package interchange
import (
 "context"
 "os"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type fitAlignmentOriginStub struct{origin ExportBodyDependencyOrigin;calls int;change bool}
func(s *fitAlignmentOriginStub)LookupExportBodyFitAlignmentOrigin(_ context.Context,_ v2input.Entry,raw []byte,fit ExportClosureNode,_ v2input.Entry)(ExportBodyDependencyOrigin,error){
 s.calls++;raw[0]='!';fit.Body[0]='!';out:=s.origin;if s.change&&s.calls>2{out.Producer.Fence++};return out,nil
}
type fitAlignmentPolicyStub struct{calls,deny int}
func(s *fitAlignmentPolicyStub)QualifyExportBodyFitAlignmentProfile(_ context.Context,_ ExportBodyDependencyOrigin,_ v2input.Entry,raw []byte,fit ExportClosureNode,_ v2input.Entry,params []byte)error{
 s.calls++;raw[0]='!';fit.Body[0]='!';params[0]='!';if s.calls==s.deny{return v2input.ErrInvalid};return nil
}
// Fake native receipts prove orchestration; full schemas are Python tests.
func TestNativeFitAlignmentRechecksOriginalAndPolicy(t *testing.T){
 for _,mode:=range []string{"valid","policy_before","policy_after","producer_changed","probe","output_size","cancelled"}{t.Run(mode,func(t *testing.T){
  parent,raw,image,mask:=bodyPersonFixture(t)
  ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(`{}`))}}
  alignmentRef:=ref();result:=[]byte(`{}`)
  fitRaw,err:=canonical.Marshal(map[string]any{"params":map[string]any{"pair_id":uuid.New(),"before":image.Entry.Ref,"after":ref(),"alignment":alignmentRef,"before_pose":ref(),"after_pose":ref(),"before_person":mask,"after_person":mask},"objects":[]any{}});if err!=nil{t.Fatal(err)}
  fit:=ExportClosureNode{Entry:v2input.Entry{Contract:"BodyFit",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(fitRaw)}},Body:fitRaw}
  alignment:=ExportClosureNode{Entry:v2input.Entry{Contract:"PairAlignment",Ref:alignmentRef},Body:result}
  params:=[]byte(`{}`);pin:=ref()
  source:=&fitAlignmentOriginStub{change:mode=="producer_changed",origin:ExportBodyDependencyOrigin{Producer:ExportParamsOperationEvidence{Task:uuid.New(),Operation:"pair.register",Version:"1",Attempt:1,Fence:1},Params:pin,ParamsObject:objectstore.Object{Key:"params",VersionID:"immutable",SHA256:pin.SHA256,Bytes:2,ContentType:"application/json"},Output:objectstore.Object{Key:"alignment",VersionID:"immutable",SHA256:alignmentRef.SHA256,Bytes:2,ContentType:"application/json"}}}
  if mode=="output_size"{source.origin.Output.Bytes=3}
  policy:=&fitAlignmentPolicyStub{};if mode=="policy_before"{policy.deny=1};if mode=="policy_after"{policy.deny=2}
  root:=t.TempDir();scratch:=&ExportArchiveScratch{root:root,maximum:1<<20};runtime:=rawSHA([]byte("runtime"));probe:=&exportStyleProbeStub{runtime:runtime};if mode=="probe"{probe.err=v2input.ErrInvalid}
  q,err:=NewNativeExportBodyFitAlignmentQualifier(ExportBodyFitAlignmentParamsReader{Source:source,Objects:statsParamsObjects{params}},policy,scratch,probe,map[string]string{},runtime);if err!=nil{t.Fatal(err)}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  err=q.QualifyExportBodyFitAlignment(ctx,parent,raw,fit,alignment)
  if (err==nil)!=(mode=="valid"){t.Fatal("unexpected alignment qualification",err)}
  if mode=="valid"&&(source.calls!=4||policy.calls!=2||probe.calls!=1){t.Fatal("missing final authority")}
  if (mode=="policy_before"||mode=="output_size"||mode=="cancelled")&&probe.calls!=0{t.Fatal("preflight reached probe")}
  if rawSHA(raw)!=parent.Ref.SHA256||rawSHA(fit.Body)!=fit.Entry.Ref.SHA256{t.Fatal("callback changed caller")}
  files,err:=os.ReadDir(root);if err!=nil||len(files)!=0||scratch.used!=0{t.Fatal("staging leaked",err)}
 })}
}
